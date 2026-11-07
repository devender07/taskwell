package file

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

// An avatar goes to the dev S3 store from `make up` and comes back through two redirects: the user's avatar to the
// file, the file to a presigned URL.
func TestAvatarRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	cfg := sysconfig.New(pool)
	h := New(pool, storage.New(cfg), dbtest.Auth, nil, cfg, func(context.Context, string) (any, error) { return nil, nil })
	mux := http.NewServeMux()
	h.Register(mux)
	user := dbtest.User(t, pool, "Avatar")
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "me.png")
	fw.Write(png)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/me/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Test-User", user)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}

	loc := "/api/users/" + user + "/avatar"
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, loc, nil)
		req.Header.Set("X-Test-User", user)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("GET %s = %d %s", loc, rec.Code, rec.Body)
		}
		loc = rec.Header().Get("Location")
	}
	res, err := http.Get(loc)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, png) || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("presigned GET = %d %v", res.StatusCode, res.Header)
	}
}

// A direct upload: started, PUT to the dev store like a browser would, then finished; bytes that aren't what was
// declared are refused at finish and deleted.
func TestDirectAvatar(t *testing.T) {
	pool := dbtest.Pool(t)
	cfg := sysconfig.New(pool)
	files := storage.New(cfg)
	h := New(pool, files, dbtest.Auth, nil, cfg, func(context.Context, string) (any, error) { return nil, nil })
	mux := http.NewServeMux()
	h.Register(mux)
	user := dbtest.User(t, pool, "Direct")
	var uploadSized func(body []byte, size int) string
	post := func(path string, fields map[string]string) (int, string) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, path, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Test-User", user)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	var put func(body []byte)
	upload := func(body []byte) string { return uploadSized(body, len(body)) }
	uploadSized = func(body []byte, size int) string {
		code, res := post("/api/me/uploads", map[string]string{"name": "me.png", "size": strconv.Itoa(size), "type": "image/png"})
		var s struct {
			ID      string
			URL     string
			Headers map[string]string
		}
		if code != http.StatusCreated || json.Unmarshal([]byte(res), &s) != nil {
			t.Fatalf("start = %d %s", code, res)
		}
		put = func(body []byte) {
			req, _ := http.NewRequest(http.MethodPut, s.URL, bytes.NewReader(body))
			for k, v := range s.Headers {
				req.Header.Set(k, v)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("PUT = %d", res.StatusCode)
			}
		}
		put(body)
		return s.ID
	}

	if code, res := post("/api/me/uploads", map[string]string{"name": "a.txt", "size": "5", "type": "text/plain"}); code != http.StatusBadRequest {
		t.Fatalf("start text = %d %s", code, res)
	}
	if code, res := post("/api/me/uploads", map[string]string{"name": "a.png", "size": "3000000", "type": "image/png"}); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("start big = %d %s", code, res)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	id := upload(png)
	if code, res := post("/api/me/avatar", map[string]string{"file_id": id}); code != http.StatusOK {
		t.Fatalf("finish = %d %s", code, res)
	}
	if code, res := post("/api/me/avatar", map[string]string{"file_id": id}); code != http.StatusBadRequest || !strings.Contains(res, errNoUpload) {
		t.Fatalf("finish again = %d %s", code, res)
	}
	// The PUT link still works after finishing, but only reaches the staged key: the checked file can't change.
	put([]byte("<html>swapped after the check</html>"))
	if b, err := files.Start(t.Context(), id, 8); err != nil || !bytes.Equal(b, png[:8]) {
		t.Fatalf("finished file changed: %q %v", b, err)
	}

	// Bytes of another size than declared are refused and deleted.
	id = uploadSized(png, len(png)+10)
	if code, res := post("/api/me/avatar", map[string]string{"file_id": id}); code != http.StatusBadRequest || !strings.Contains(res, errNoUpload) {
		t.Fatalf("finish wrong size = %d %s", code, res)
	}
	if obj, err := files.Head(t.Context(), id); err != nil || obj != nil {
		t.Fatalf("wrong-size object still there: %v %v", obj, err)
	}

	id = upload([]byte("<html>not a png</html>"))
	if code, res := post("/api/me/avatar", map[string]string{"file_id": id}); code != http.StatusBadRequest || !strings.Contains(res, errChooseImage) {
		t.Fatalf("finish html = %d %s", code, res)
	}
	if obj, err := files.Head(t.Context(), id); err != nil || obj != nil {
		t.Fatalf("refused object still there: %v %v", obj, err)
	}

	// Unfinished avatar uploads are capped per user.
	for i := range maxAvatarUploads + 1 {
		code, res := post("/api/me/uploads", map[string]string{"name": "me.png", "size": "10", "type": "image/png"})
		if want := http.StatusCreated; i == maxAvatarUploads {
			want = http.StatusTooManyRequests
			if code != want {
				t.Fatalf("upload %d = %d %s", i, code, res)
			}
		} else if code != want {
			t.Fatalf("upload %d = %d %s", i, code, res)
		}
	}
	// Expired ones are reaped when the next starts, freeing the cap.
	if _, err := pool.Exec(t.Context(), `update uploads set created_at = now() - interval '2 hours' where user_id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if code, res := post("/api/me/uploads", map[string]string{"name": "me.png", "size": "10", "type": "image/png"}); code != http.StatusCreated {
		t.Fatalf("upload after expiry = %d %s", code, res)
	}

	// Starts at the same moment still respect the cap.
	if _, err := pool.Exec(t.Context(), `delete from uploads where user_id = $1`, user); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 3*maxAvatarUploads)
	for range cap(codes) {
		wg.Go(func() {
			code, _ := post("/api/me/uploads", map[string]string{"name": "me.png", "size": "10", "type": "image/png"})
			codes <- code
		})
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == http.StatusCreated {
			created++
		}
	}
	if created != maxAvatarUploads {
		t.Fatalf("concurrent starts created %d, want %d", created, maxAvatarUploads)
	}
}

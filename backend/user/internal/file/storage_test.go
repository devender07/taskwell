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
	upload := func(body []byte) string {
		code, res := post("/api/me/uploads", map[string]string{"name": "me.png", "size": strconv.Itoa(len(body)), "type": "image/png"})
		var s struct {
			ID      string
			URL     string
			Headers map[string]string
		}
		if code != http.StatusCreated || json.Unmarshal([]byte(res), &s) != nil {
			t.Fatalf("start = %d %s", code, res)
		}
		req, _ := http.NewRequest(http.MethodPut, s.URL, bytes.NewReader(body))
		for k, v := range s.Headers {
			req.Header.Set(k, v)
		}
		put, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		put.Body.Close()
		if put.StatusCode != http.StatusOK {
			t.Fatalf("PUT = %d", put.StatusCode)
		}
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

	id = upload([]byte("<html>not a png</html>"))
	if code, res := post("/api/me/avatar", map[string]string{"file_id": id}); code != http.StatusBadRequest || !strings.Contains(res, errChooseImage) {
		t.Fatalf("finish html = %d %s", code, res)
	}
	if obj, err := files.Head(t.Context(), id); err != nil || obj != nil {
		t.Fatalf("refused object still there: %v %v", obj, err)
	}
}

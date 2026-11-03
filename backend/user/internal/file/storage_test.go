package file

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/storage"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
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

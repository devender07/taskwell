package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPurgeURL: the purge is Cloudflare's single-file purge of the zone with the token; anything but a 200 with
// success true is an error.
func TestPurgeURL(t *testing.T) {
	var got, auth, path string
	reply := `{"success":true,"errors":[],"messages":[],"result":{"id":"z1"}}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, auth, path = string(b), r.Header.Get("Authorization"), r.Method+" "+r.URL.Path
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	defer srv.Close()
	old := cloudflareAPI
	cloudflareAPI = srv.URL
	defer func() { cloudflareAPI = old }()

	if err := purgeURL(context.Background(), "z1", "tok", "https://files.test/abc"); err != nil {
		t.Fatal(err)
	}
	if got != `{"files":["https://files.test/abc"]}` || auth != "Bearer tok" || path != "POST /zones/z1/purge_cache" {
		t.Fatalf("request %s %q %q", path, auth, got)
	}
	reply = `{"success":false,"errors":[{"code":1134}]}`
	if err := purgeURL(context.Background(), "z1", "tok", "https://files.test/abc"); err == nil {
		t.Fatal("success false wasn't an error")
	}
	reply, status = `{"success":true}`, http.StatusForbidden
	if err := purgeURL(context.Background(), "z1", "tok", "https://files.test/abc"); err == nil {
		t.Fatal("a 403 wasn't an error")
	}
}

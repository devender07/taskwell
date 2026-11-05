package storage

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

// Runs against the s3_* defaults: the dev S3 store from `make up`.
func TestRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := New(sysconfig.New(dbtest.Pool(t)))
	id, body := rand.Text(), []byte("hello")
	if err := s.Put(ctx, id, "text/plain", `attachment; filename="a.txt"`, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	u, err := s.URL(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(got, body) || res.Header.Get("Content-Type") != "text/plain" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="a.txt"` {
		t.Fatalf("get = %d %q %v", res.StatusCode, got, res.Header)
	}

	if err := s.SetHeaders(ctx, id, "text/plain", `attachment; filename="b.txt"`); err != nil {
		t.Fatal(err)
	}
	if res, err := http.Get(u); err != nil || res.Header.Get("Content-Disposition") != `attachment; filename="b.txt"` {
		t.Fatalf("after SetHeaders: %v %v", res, err)
	}

	var old []string
	if err := s.Old(ctx, time.Now().Add(time.Minute), func(keys []string) error { old = append(old, keys...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(old, id) {
		t.Fatalf("Old missed %s", id)
	}

	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if res, err := http.Get(u); err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: %v %v", res.StatusCode, err)
	}
}

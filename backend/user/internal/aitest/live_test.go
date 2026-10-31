package aitest

import (
	"bufio"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	l := NewLive()
	mux := http.NewServeMux()
	mux.Handle("GET /live/{run}", l)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	l.publish("r1", []byte("first"))
	resp, err := http.Get(srv.URL + "/live/r1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := multipart.NewReader(bufio.NewReader(resp.Body), params["boundary"])
	next := func() string {
		p, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		return string(b)
	}
	if got := next(); got != "first" {
		t.Fatalf("late viewer gets the last frame: got %q", got)
	}
	go func() {
		time.Sleep(50 * time.Millisecond) // let the viewer wait on the channel
		l.publish("r1", []byte("second"))
		time.Sleep(50 * time.Millisecond)
		l.end("r1")
	}()
	if got := next(); got != "second" {
		t.Fatalf("got %q, want second", got)
	}
	if _, err := parts.NextPart(); err == nil {
		t.Fatal("stream should end with the run")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.runs) != 0 {
		t.Fatalf("feeds left after end: %d", len(l.runs))
	}
}

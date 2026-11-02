package aitest

import (
	"bufio"
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

// TestLiveSkipsAhead: a viewer still busy with a frame gets the newest one next, not one that went stale meanwhile.
func TestLiveSkipsAhead(t *testing.T) {
	l := NewLive()
	ch := make(chan []byte, 1)
	l.feed("r1").viewers[ch] = true
	l.publish("r1", []byte("old"))
	l.publish("r1", []byte("new"))
	if got := string(<-ch); got != "new" {
		t.Fatalf("viewer got %q, want new", got)
	}
}

// TestLiveFramesFlow: a real page reaches a viewer a few times a second, and isn't captured with nobody watching.
func TestLiveFramesFlow(t *testing.T) {
	chromeURL, _, _ := testBrowser(t)
	ctx := context.Background()
	c, err := dialChrome(ctx, chromeURL)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLive()
	p, err := c.newPage(ctx, "", "r1", "t", false, l)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	time.Sleep(time.Second)
	if l.runs["r1"] != nil {
		t.Fatal("frames taken with nobody watching")
	}
	ch := make(chan []byte, 1)
	l.mu.Lock()
	l.feed("r1").viewers[ch] = true
	l.mu.Unlock()
	var n atomic.Int32
	go func() {
		for range ch {
			n.Add(1)
		}
	}()
	time.Sleep(2 * time.Second)
	l.end("r1")
	if got := n.Load(); got < 4 {
		t.Fatalf("%d frames in 2 s", got)
	}
}

// TestLiveFramesWhole: a step's screenshot, which scales the page while it's taken, never shrinks a live frame.
func TestLiveFramesWhole(t *testing.T) {
	chromeURL, _, _ := testBrowser(t)
	ctx := context.Background()
	c, err := dialChrome(ctx, chromeURL)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLive()
	p, err := c.newPage(ctx, "", "r1", "t", false, l)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	var ok bool
	if err := p.eval(ctx, `document.body.innerHTML = '<div style="position:fixed;inset:0;width:100vw;height:100vh;background:red"></div>', true`, &ok); err != nil {
		t.Fatal(err)
	}
	ch := make(chan []byte, 1)
	l.mu.Lock()
	l.feed("r1").viewers[ch] = true
	l.mu.Unlock()
	end := time.Now().Add(3 * time.Second)
	go func() {
		for time.Now().Before(end) {
			_, _ = p.screenshot(ctx)
		}
	}()
	for time.Now().Before(end) {
		img, err := jpeg.Decode(bytes.NewReader(<-ch))
		if err != nil {
			t.Fatal(err)
		}
		if r, g, _, _ := img.At(viewportW-5, viewportH-5).RGBA(); r < 0xc000 || g > 0x4000 {
			t.Fatal("live frame shrunk: its corner isn't the page")
		}
	}
}

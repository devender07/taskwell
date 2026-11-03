package aitest

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Live fans each run's frames (JPEG) out to viewers as an MJPEG stream, which an <img> plays natively.
// The tester serves it on an internal port; the user API checks membership and proxies to it.
type Live struct {
	mu   sync.Mutex
	runs map[string]*feed
}

type feed struct {
	last    []byte
	viewers map[chan []byte]bool
}

func NewLive() *Live { return &Live{runs: map[string]*feed{}} }

func (l *Live) feed(runID string) *feed {
	f := l.runs[runID]
	if f == nil {
		f = &feed{viewers: map[chan []byte]bool{}}
		l.runs[runID] = f
	}
	return f
}

// publish sends a frame to the run's viewers; a viewer still busy with the previous frame gets this one instead of
// the one waiting, so a slow viewer skips ahead rather than stopping on an old frame.
func (l *Live) publish(runID string, frame []byte) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.feed(runID)
	f.last = frame
	for ch := range f.viewers {
		select {
		case <-ch:
		default:
		}
		ch <- frame // never blocks: publish is the only sender, under l.mu, and the buffer is empty
	}
}

// watched reports whether anyone is viewing the run, so frames are only taken for someone.
func (l *Live) watched(runID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.runs[runID]
	return f != nil && len(f.viewers) > 0
}

// end closes the run's streams.
func (l *Live) end(runID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if f := l.runs[runID]; f != nil {
		for ch := range f.viewers {
			close(ch)
		}
		delete(l.runs, runID)
	}
}

// ServeHTTP streams GET /live/{run} until the run ends or the viewer leaves.
func (l *Live) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run")
	ch := make(chan []byte, 1)
	l.mu.Lock()
	f := l.feed(runID)
	f.viewers[ch] = true
	if f.last != nil {
		ch <- f.last
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		if f := l.runs[runID]; f != nil && f.viewers[ch] {
			delete(f.viewers, ch)
			if len(f.viewers) == 0 && f.last == nil { // a viewer of a run this tester never streamed
				delete(l.runs, runID)
			}
		}
		l.mu.Unlock()
	}()

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// Each frame is followed by the next boundary, so the browser shows it now instead of when the next one comes.
	if _, err := io.WriteString(w, "--frame\r\n"); err != nil || rc.Flush() != nil {
		return
	}
	idle := time.NewTimer(15 * time.Minute) // a run never lasts this long without a frame; don't hold a dead stream
	defer idle.Stop()
	for {
		select {
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "Content-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n--frame\r\n"); err != nil || rc.Flush() != nil {
				return
			}
			idle.Reset(15 * time.Minute)
		case <-idle.C:
			return
		case <-r.Context().Done():
			return
		}
	}
}

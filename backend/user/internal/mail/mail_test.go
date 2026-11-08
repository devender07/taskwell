package mail

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/db"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

func TestRetryWait(t *testing.T) {
	base := 30 * time.Second
	for attempts, want := range map[int]time.Duration{
		1: 30 * time.Second, 2: time.Minute, 3: 2 * time.Minute, 7: 32 * time.Minute, 8: time.Hour, 30: time.Hour, 64: time.Hour,
	} {
		if got := retryWait(base, attempts); got != want {
			t.Errorf("retryWait(%v, %d) = %v, want %v", base, attempts, got, want)
		}
	}
}

// fakeSMTP accepts everything except RCPT to addresses starting with "bad", recording delivered recipients.
// With silent, it accepts connections and never answers.
func fakeSMTP(t *testing.T, silent bool) (port string, delivered func() []string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var got []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if silent {
					time.Sleep(time.Minute)
					return
				}
				r := bufio.NewReader(c)
				say := func(s string) { c.Write([]byte(s + "\r\n")) }
				say("220 fake")
				var rcpt string
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"), strings.HasPrefix(cmd, "MAIL"):
						say("250 ok")
					case strings.HasPrefix(cmd, "RCPT"):
						if strings.Contains(cmd, "<BAD") {
							say("550 no such user")
							continue
						}
						rcpt = strings.ToLower(strings.Trim(strings.TrimPrefix(cmd, "RCPT TO:"), "<>"))
						say("250 ok")
					case cmd == "DATA":
						say("354 go")
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
						}
						mu.Lock()
						got = append(got, rcpt)
						mu.Unlock()
						say("250 queued")
					case cmd == "QUIT":
						say("221 bye")
						return
					default:
						say("250 ok")
					}
				}
			}()
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }
}

// TestQueue runs against a scratch database, e.g.
// TEST_DATABASE_URL=postgres://taskwell:taskwell@localhost:5433/taskwell_test?sslmode=disable (its outbound_mail and SMTP
// settings get overwritten; never point it at a database an API instance uses).
func TestQueue(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	setSMTP := func(port string) {
		if _, err := pool.Exec(ctx, `update system_config set value = case key when 'smtp_host' then '127.0.0.1' when 'smtp_port' then $1
			when 'smtp_from' then 'Taskwell <noreply@taskwell.test>' else '' end
			where key in ('smtp_host', 'smtp_port', 'smtp_from', 'smtp_username', 'smtp_password')`, port); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() {
		if _, err := pool.Exec(ctx, `delete from outbound_mail`); err != nil {
			t.Fatal(err)
		}
	}
	type row struct {
		attempts int
		failed   bool
		lastErr  string
		due      bool
	}
	read := func(to string) row {
		var r row
		if err := pool.QueryRow(ctx, `select attempts, failed_at is not null, coalesce(last_error, ''), next_attempt_at <= now()
			from outbound_mail where recipient = $1`, to).Scan(&r.attempts, &r.failed, &r.lastErr, &r.due); err != nil {
			t.Fatalf("read %s: %v", to, err)
		}
		return r
	}
	drain := func(m *Mailer) {
		for {
			sent, err := m.sendNext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !sent {
				return
			}
		}
	}

	t.Run("a failing address backs off and gives up without blocking others", func(t *testing.T) {
		reset()
		port, delivered := fakeSMTP(t, false)
		setSMTP(port)
		m := New(pool, sysconfig.New(pool), 5*time.Second, time.Hour, 2)
		for _, to := range []string{"bad@taskwell.test", "good@taskwell.test"} {
			if err := m.Enqueue(ctx, pool, to, "Hello", "Body"); err != nil {
				t.Fatal(err)
			}
		}
		drain(m)
		if got := delivered(); len(got) != 1 || got[0] != "good@taskwell.test" {
			t.Fatalf("delivered %v, want good@taskwell.test", got)
		}
		if r := read("bad@taskwell.test"); r.attempts != 1 || r.failed || r.due || !strings.Contains(r.lastErr, "550") {
			t.Fatalf("after one failure: %+v, want attempts 1, waiting, with the 550", r)
		}
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from outbound_mail where recipient = 'good@taskwell.test'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("sent row kept: %d %v", n, err)
		}
		if _, err := pool.Exec(ctx, `update outbound_mail set next_attempt_at = now()`); err != nil {
			t.Fatal(err)
		}
		drain(m)
		if r := read("bad@taskwell.test"); r.attempts != 2 || !r.failed {
			t.Fatalf("after max attempts: %+v, want given up", r)
		}
		if _, err := pool.Exec(ctx, `update outbound_mail set next_attempt_at = now()`); err != nil {
			t.Fatal(err)
		}
		if sent, err := m.sendNext(ctx); sent || err != nil {
			t.Fatalf("given-up row claimed again: %v %v", sent, err)
		}
	})

	t.Run("a silent server times out and the claim holds the row", func(t *testing.T) {
		reset()
		port, _ := fakeSMTP(t, true)
		setSMTP(port)
		m := New(pool, sysconfig.New(pool), 300*time.Millisecond, time.Hour, 5)
		if err := m.Enqueue(ctx, pool, "slow@taskwell.test", "Hello", "Body"); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		drain(m)
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("send took %v, want about the 300ms timeout", d)
		}
		if r := read("slow@taskwell.test"); r.attempts != 1 || r.failed || r.due || r.lastErr == "" {
			t.Fatalf("after timeout: %+v", r)
		}
	})
}

// TestCompose: with an HTML part the message is multipart/alternative, plain text first, both parts decode back to
// what was queued; without one it's plain text. Render escapes what it's given.
func TestCompose(t *testing.T) {
	long := strings.Repeat("x", 2000)
	html, err := Render(Email{Heading: "Ann <script>", Code: "123456", Card: &Card{Label: "ENG-1", Title: `"Fix" & <b>it</b>`},
		Button: "Open", URL: "javascript:alert(1)", Note: long})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "<b>it</b>") || strings.Contains(html, `href="javascript`) || !strings.Contains(html, `href="#ZgotmplZ"`) ||
		!strings.Contains(html, "123456") || !strings.Contains(html, "&lt;b&gt;it&lt;/b&gt;") {
		t.Fatalf("render didn't escape: %s", html)
	}

	raw := compose("Taskwell <no-reply@taskwell.test>", "ann@example.test", "Your code: 123456", "Your code is 123456\n", html)
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a %d-byte line", len(line))
		}
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	typ, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || typ != "multipart/alternative" {
		t.Fatalf("content type %q %v", typ, err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	var got []string
	for {
		p, err := r.NextPart() // decodes quoted-printable
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p)
		got = append(got, p.Header.Get("Content-Type")+"|"+strings.ReplaceAll(string(b), "\r\n", "\n"))
	}
	if len(got) != 2 || got[0] != "text/plain; charset=UTF-8|Your code is 123456\n" || got[1] != "text/html; charset=UTF-8|"+html {
		t.Fatalf("parts %q", got)
	}

	plain := compose("Taskwell <no-reply@taskwell.test>", "ann@example.test", "Hi", "Hello "+long+"\n", "")
	msg, err = netmail.ReadMessage(bytes.NewReader(plain))
	if err != nil || !strings.HasPrefix(msg.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("plain: %v %v", msg.Header, err)
	}
	b, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if strings.ReplaceAll(string(b), "\r\n", "\n") != "Hello "+long+"\n" {
		t.Fatalf("plain body didn't round-trip")
	}
}

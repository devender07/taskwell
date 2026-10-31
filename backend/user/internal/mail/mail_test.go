package mail

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
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
// TEST_DATABASE_URL=postgres://tether:tether@localhost:5433/tether_test?sslmode=disable (its outbound_mail and SMTP
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
			when 'smtp_from' then 'Tether <noreply@tether.test>' else '' end
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
		for _, to := range []string{"bad@tether.test", "good@tether.test"} {
			if err := m.Enqueue(ctx, pool, to, "Hello", "Body"); err != nil {
				t.Fatal(err)
			}
		}
		drain(m)
		if got := delivered(); len(got) != 1 || got[0] != "good@tether.test" {
			t.Fatalf("delivered %v, want good@tether.test", got)
		}
		if r := read("bad@tether.test"); r.attempts != 1 || r.failed || r.due || !strings.Contains(r.lastErr, "550") {
			t.Fatalf("after one failure: %+v, want attempts 1, waiting, with the 550", r)
		}
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from outbound_mail where recipient = 'good@tether.test'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("sent row kept: %d %v", n, err)
		}
		if _, err := pool.Exec(ctx, `update outbound_mail set next_attempt_at = now()`); err != nil {
			t.Fatal(err)
		}
		drain(m)
		if r := read("bad@tether.test"); r.attempts != 2 || !r.failed {
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
		if err := m.Enqueue(ctx, pool, "slow@tether.test", "Hello", "Body"); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		drain(m)
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("send took %v, want about the 300ms timeout", d)
		}
		if r := read("slow@tether.test"); r.attempts != 1 || r.failed || r.due || r.lastErr == "" {
			t.Fatalf("after timeout: %+v", r)
		}
	})
}

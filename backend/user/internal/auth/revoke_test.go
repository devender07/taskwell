package auth

import (
	"testing"
	"time"
)

func TestAliveCache(t *testing.T) {
	c := newAliveCache()
	t0 := time.Now()
	if _, ok := c.get("u1", "s1", t0); ok {
		t.Fatal("empty cache hit")
	}
	c.put("u1", "s1", true, t0)
	c.put("u1", "s2", false, t0)
	if alive, ok := c.get("u1", "s1", t0.Add(aliveTTL-time.Second)); !ok || !alive {
		t.Error("live sign-in not cached")
	}
	if alive, ok := c.get("u1", "s2", t0); !ok || alive {
		t.Error("ended sign-in not cached as ended")
	}
	if _, ok := c.get("u1", "s1", t0.Add(aliveTTL)); ok {
		t.Error("entry outlives aliveTTL")
	}

	// A revocation drops the user's entries at once, and a check that started before it isn't stored afterwards.
	c.forget("u1", t0.Add(time.Second))
	if _, ok := c.get("u1", "s1", t0.Add(time.Second)); ok {
		t.Error("forget kept an entry")
	}
	c.put("u1", "s1", true, t0) // the query ran before the revocation committed
	if _, ok := c.get("u1", "s1", t0.Add(2*time.Second)); ok {
		t.Error("a stale check was stored after forget")
	}
	c.put("u1", "s1", false, t0.Add(2*time.Second))
	if alive, ok := c.get("u1", "s1", t0.Add(2*time.Second)); !ok || alive {
		t.Error("a check after forget wasn't stored")
	}

	c.put("u2", "s1", true, t0)
	c.prune(t0.Add(aliveTTL + 3*time.Second))
	if len(c.entries) != 0 || len(c.cleared) != 0 {
		t.Errorf("prune left %d users, %d clears", len(c.entries), len(c.cleared))
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/invite?token=abc": "/invite?token=abc",
		"/":                 "/",
		"":                  "/signin",
		"//evil.com":        "/signin",
		"/\\evil.com":       "/signin",
		"https://evil.com":  "/signin",
		"/\t/evil.com":      "/signin",
		"/\n/evil.com":      "/signin",
		"invite":            "/signin",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

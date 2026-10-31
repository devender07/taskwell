package auth

import (
	"sync"
	"time"
)

// aliveTTL is how long a sign-in's checked state is reused. Revocations clear it at once on every instance (Forget,
// through the realtime listener), so the TTL only bounds a missed notification.
const aliveTTL = 30 * time.Second

// aliveCache remembers whether sign-ins (user + sid) are live, so Authenticate queries at most once per aliveTTL.
type aliveCache struct {
	mu      sync.Mutex
	entries map[string]map[string]aliveEntry // by user, then sid
	cleared map[string]time.Time             // by user: the last Forget, so a check that started before it isn't stored
}

type aliveEntry struct {
	alive   bool
	checked time.Time // when the query started
}

const maxAliveEntries = 100_000 // users; past it, expired entries are pruned

func newAliveCache() *aliveCache {
	return &aliveCache{entries: map[string]map[string]aliveEntry{}, cleared: map[string]time.Time{}}
}

// get returns the remembered state; ok is false when there is none from the last aliveTTL.
func (c *aliveCache) get(userID, sid string, now time.Time) (alive, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[userID][sid]
	if !ok || now.Sub(e.checked) >= aliveTTL {
		return false, false
	}
	return e.alive, true
}

// put remembers a check that started at checked, unless the user's sign-ins were forgotten since.
func (c *aliveCache) put(userID, sid string, alive bool, checked time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.cleared[userID].Before(checked) {
		return
	}
	if len(c.entries) >= maxAliveEntries {
		c.prune(checked)
	}
	if c.entries[userID] == nil {
		c.entries[userID] = map[string]aliveEntry{}
	}
	c.entries[userID][sid] = aliveEntry{alive: alive, checked: checked}
}

// forget drops everything remembered about the user's sign-ins.
func (c *aliveCache) forget(userID string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, userID)
	if len(c.cleared) >= maxAliveEntries {
		c.prune(now)
	}
	c.cleared[userID] = now
}

// prune drops expired entries and old clears (a check runs far shorter than aliveTTL). Called with mu held.
func (c *aliveCache) prune(now time.Time) {
	for user, sids := range c.entries {
		for sid, e := range sids {
			if now.Sub(e.checked) >= aliveTTL {
				delete(sids, sid)
			}
		}
		if len(sids) == 0 {
			delete(c.entries, user)
		}
	}
	for k, at := range c.cleared {
		if now.Sub(at) >= aliveTTL {
			delete(c.cleared, k)
		}
	}
}

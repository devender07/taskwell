package realtime

// OnRevoke sets fn to run with the user's id whenever a session.revoked event reaches this instance (sign-out,
// password change or reset, suspension), before their sockets close. Set it before Listen.
// ponytail: events published while the listener reconnects are lost, so a cache fn keeps must expire on its own.
func (h *Handler) OnRevoke(fn func(userID string)) { h.onRevoke = fn }

func (h *Handler) revoked(userID string) {
	if h.onRevoke != nil {
		h.onRevoke(userID)
	}
}

// MaxConnsPerUser caps one user's sockets on this instance; main sets it from WS_MAX_CONNS_PER_USER.
// ponytail: per instance, so N instances allow N times as many; count in Postgres presence if that matters.
var MaxConnsPerUser = 20

// userCount returns how many sockets the user has here.
func (hb *hub) userCount(userID string) int {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return len(hb.users[userID])
}

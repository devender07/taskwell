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

// MaxConnsPerUser caps one user's sockets across every instance (counted in realtime_conns); main sets it from
// WS_MAX_CONNS_PER_USER.
var MaxConnsPerUser = 20

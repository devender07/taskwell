package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// typingAccessTTL is how long a connection trusts a chat access check.
	typingAccessTTL = 60 * time.Second
	// typingEvery drops typing frames for the same chat and thread closer together than this (clients send every 3s).
	typingEvery = time.Second
	// typingChats caps a connection's cached checks; the cache starts over past it.
	typingChats = 100
)

// chatAccess is one connection's cached check of a chat: who sees it (nil when the user can't) and when it last
// relayed typing per thread.
type chatAccess struct {
	audience []string
	name     string
	checked  time.Time
	sent     map[string]time.Time
}

// typing relays "the user is typing" in the chat (and thread, a root message id, or nil) to everyone who sees it but
// the user's own connections. A chat the user can't open, or a frame too soon after the last, is dropped silently.
func (h *Handler) typing(ctx context.Context, c *Conn, chatID string, thread *string) error {
	if h.ChatAudience == nil || chatID == "" || len(chatID) > 64 || thread != nil && len(*thread) > 64 {
		return nil
	}
	a := c.chats[chatID]
	if a == nil || time.Since(a.checked) > typingAccessTTL {
		aud, err := h.ChatAudience(ctx, c.userID, chatID)
		if err != nil {
			return err
		}
		name := ""
		if aud != nil {
			if name, err = h.model.userName(ctx, c.userID); err != nil {
				return err
			}
		}
		if len(c.chats) >= typingChats {
			clear(c.chats)
		}
		a = &chatAccess{audience: aud, name: name, checked: time.Now(), sent: map[string]time.Time{}}
		c.chats[chatID] = a
	}
	if len(a.audience) == 0 {
		return nil
	}
	key := ""
	if thread != nil {
		key = *thread
	}
	if time.Since(a.sent[key]) < typingEvery {
		return nil
	}
	a.sent[key] = time.Now()
	ev, err := json.Marshal(map[string]any{"type": "typing", "chat": chatID, "thread": thread,
		"user": map[string]string{"id": c.userID, "name": a.name}})
	if err != nil {
		return err
	}
	ps := make([]string, len(a.audience))
	for i, aud := range a.audience {
		p, err := json.Marshal(envelope{Aud: aud, Skip: c.userID, Event: ev})
		if err != nil {
			return err
		}
		ps[i] = string(p)
	}
	if err := h.model.notify(ctx, ps); err != nil {
		return fmt.Errorf("relay typing: %w", err)
	}
	return nil
}

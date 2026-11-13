package realtime

import (
	"context"
	"testing"
)

func TestDeliverConfigChanged(t *testing.T) {
	calls := 0
	h := &Handler{hub: newHub(), ConfigChanged: func() { calls++ }}
	h.deliver(context.Background(), `{"aud":"admin","event":{"kind":"admin.user"}}`, nil)
	h.deliver(context.Background(), `{"aud":"admin","event":{"kind":"admin.config"}}`, nil)
	if calls != 1 {
		t.Fatalf("ConfigChanged ran %d times, want once (for admin.config only)", calls)
	}
}

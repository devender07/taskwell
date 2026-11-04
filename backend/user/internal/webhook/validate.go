package webhook

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/netguard"
)

// Events are the events a webhook can subscribe to; ping is sent only by "Send test".
var Events = []string{"issue.created", "issue.updated", "issue.deleted", "comment.created"}

const maxURLLen = 2000

// checkURL returns the trimmed URL or its field message. http is accepted only when allowHTTP (outside production),
// and a host that is or resolves to a private address only when allowPrivate (dev). A name that doesn't resolve
// passes: the worker checks the address again when it dials.
func checkURL(ctx context.Context, resolve netguard.Resolver, raw string, allowHTTP, allowPrivate bool) (string, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "Enter a URL."
	}
	u, err := url.Parse(s)
	if err != nil || len(s) > maxURLLen || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		(u.Scheme != "https" && u.Scheme != "http") {
		return "", "Enter a valid URL."
	}
	if u.Scheme == "http" && !allowHTTP {
		return "", "Use an https URL."
	}
	if allowPrivate {
		return s, ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if netguard.PrivateHost(host) {
		return "", msgPrivate
	}
	if _, err := netguard.Addrs(ctx, resolve, host, false); errors.As(err, new(netguard.PrivateError)) {
		return "", msgPrivate
	}
	return s, ""
}

const msgPrivate = "This URL points to a private address."

// checkEvents returns the chosen events, de-duplicated in Events order, or the field message.
func checkEvents(vs []string) ([]string, string) {
	var out []string
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !slices.Contains(Events, v) {
			return nil, "Choose events from the list."
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, "Choose at least one event."
	}
	return slices.DeleteFunc(slices.Clone(Events), func(e string) bool { return !slices.Contains(out, e) }), ""
}

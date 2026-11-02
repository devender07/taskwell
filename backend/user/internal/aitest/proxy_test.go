package aitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func TestAllowed(t *testing.T) {
	sites := []string{"acme.com", "staging.example.org"}
	for _, c := range []struct {
		host string
		want bool
	}{
		{"acme.com", true},
		{"ACME.com.", true},
		{"app.acme.com", true},
		{"evil-acme.com", false},
		{"acme.com.evil.net", false},
		{"example.org", false},
		{"staging.example.org", true},
		{"10.0.0.1", false},
	} {
		if got := allowedHost(c.host, sites); got != c.want {
			t.Errorf("allowedHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
	for _, c := range []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"172.16.5.4", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"::ffff:127.0.0.1", true},
		{"0.0.0.0", true},
		{"fd00::1", true},
		{"fe80::1", true},
		{"224.0.0.1", true},
		{"64:ff9b::a00:1", true},
		{"93.184.216.34", false},
		{"2606:4700::1111", false},
	} {
		if got := blockedAddr(netip.MustParseAddr(c.ip)); got != c.want {
			t.Errorf("blockedAddr(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
	for _, c := range []struct{ raw, msg string }{
		{"https://staging.acme.com/", ""},
		{"http://staging.acme.com", "Enter a URL starting with https://."},
		{"ftp://acme.com", "Enter a URL starting with https://."},
		{"https://user:pw@acme.com", "Enter a URL starting with https://."},
		{"https://localhost:3000", "Enter a public address, not a private or local one."},
		{"https://10.1.2.3", "Enter a public address, not a private or local one."},
		{"https://[::1]:8080", "Enter a public address, not a private or local one."},
	} {
		if _, _, msg := checkBaseURL(c.raw, false, false); msg != c.msg {
			t.Errorf("checkBaseURL(%q) = %q, want %q", c.raw, msg, c.msg)
		}
	}
	if base, host, _ := checkBaseURL(" HTTPS://Staging.Acme.com/app/?x=1 ", false, false); base != "https://staging.acme.com/app" || host != "staging.acme.com" {
		t.Errorf("normalized %q %q", base, host)
	}
}

// TestProxyRefuses checks that the proxy asks for credentials, and refuses an unknown run, a host that isn't a
// site, and a site whose name resolves to a private address (DNS pointing at the metadata service).
func TestProxyRefuses(t *testing.T) {
	p := NewProxy(false)
	p.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	p.open("run1", "tok", []string{"acme.com"})
	srv := httptest.NewServer(p)
	defer srv.Close()
	get := func(user *url.Userinfo, target string) int {
		t.Helper()
		pu, _ := url.Parse(srv.URL)
		pu.User = user
		c := http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
		resp, err := c.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct {
		user   *url.Userinfo
		target string
		want   int
	}{
		{nil, "http://acme.com/", 407},
		{url.UserPassword("run1", "wrong"), "http://acme.com/", 403},
		{url.UserPassword("run1", "tok"), "http://evil.com/", 403},
		{url.UserPassword("run1", "tok"), "http://acme.com/", 403}, // resolves to 169.254.169.254
		{url.UserPassword("run1", "tok"), "http://127.0.0.1/", 403},
	} {
		if got := get(c.user, c.target); got != c.want {
			t.Errorf("%v %s: %d, want %d", c.user, c.target, got, c.want)
		}
	}
	if b := p.takeBlocked("run1"); len(b) != 3 || b[0] != "evil.com" {
		t.Errorf("blocked %v", b)
	}
}

// TestAccountFor checks that an account tied to a site gives its password only to that site's host and subdomains.
func TestAccountFor(t *testing.T) {
	accounts := []accountRef{{id: "1", label: "Any"}, {id: "2", label: "Shop", host: "shop.test"}}
	for _, c := range []struct {
		label, host, want string
	}{
		{"Any", "other.test", "1"},
		{"Shop", "shop.test", "2"},
		{"Shop", "app.shop.test", "2"},
		{"Shop", "evil-shop.test", ""},
		{"Shop", "other.test", ""},
		{"Missing", "shop.test", ""},
	} {
		a, err := accountFor(accounts, c.label, c.host)
		got := ""
		if err == nil {
			got = a.id
		}
		if got != c.want {
			t.Errorf("accountFor(%q, %q) = %q, %v; want %q", c.label, c.host, got, err, c.want)
		}
	}
}

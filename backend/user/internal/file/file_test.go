package file

import (
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\shot.png`, "shot.png"},
		{"a\x00b\nc\u202e.txt", "abc.txt"},
		{"bad\xffutf8.txt", "badutf8.txt"},
		{"  ", "file"},
		{"..", "file"},
		{"dir/", "file"},
		{strings.Repeat("é", 250), strings.Repeat("é", 200)},
	}
	for _, c := range cases {
		if got := CleanName(c.in); got != c.want {
			t.Errorf("CleanName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := URL("id", "a b?#/.txt"); got != "/api/files/id/a%20b%3F%23%2F.txt" {
		t.Errorf("URL = %q", got)
	}
	for _, c := range []struct{ typ, name, want string }{
		{"image/png", "x.png", "inline"},
		{"text/xml; charset=utf-8", "logo.svg", "attachment; filename*=UTF-8''logo.svg"},
		{"text/plain; charset=utf-8", `a "b";c=d é+.txt`, "attachment; filename*=UTF-8''a%20%22b%22%3Bc%3Dd%20%C3%A9%2B.txt"},
	} {
		if got := Disposition(c.typ, c.name); got != c.want {
			t.Errorf("Disposition(%q, %q) = %q, want %q", c.typ, c.name, got, c.want)
		}
	}
}

// The file Worker checks the same vector (cdn/sign.check.mjs).
func TestSign(t *testing.T) {
	got := sign("test-secret", "/0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b/photo%201.png", 1767312000)
	if want := "7EzqQ3f5boRpA14Ux8-zYIfFyL4IfxF2bb2GA55QiUQ"; got != want {
		t.Fatalf("sign = %q, want %q", got, want)
	}
}

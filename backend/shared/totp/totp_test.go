package totp

import (
	"strings"
	"testing"
	"time"
)

// rfcSecret is RFC 6238's SHA-1 test key, "12345678901234567890", in base32.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPCode(t *testing.T) {
	// RFC 6238 appendix B, last 6 of the 8 digits.
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		got, err := Code(rfcSecret, unix/30)
		if err != nil || got != want {
			t.Errorf("Code at %d = %q, %v; want %q", unix, got, err, want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	now := time.Unix(1234567890, 0)
	cur := now.Unix() / 30
	code, _ := Code(rfcSecret, cur)
	prev, _ := Code(rfcSecret, cur-1)
	old, _ := Code(rfcSecret, cur-2)
	tests := []struct {
		name     string
		code     string
		lastStep int64
		ok       bool
	}{
		{"current", code, 0, true},
		{"with spaces", code[:3] + " " + code[3:], 0, true},
		{"previous step (drift)", prev, 0, true},
		{"two steps old", old, 0, false},
		{"replayed", code, cur, false},
		{"wrong", "000000", 0, false},
		{"too short", code[:5], 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step, ok, err := Check(rfcSecret, tt.code, now, tt.lastStep)
			if err != nil || ok != tt.ok || (ok && step <= tt.lastStep) {
				t.Errorf("Check(%q) = %d, %v, %v; want ok=%v", tt.code, step, ok, err, tt.ok)
			}
		})
	}
	if _, _, err := Check("not base32!", "123456", now, 0); err == nil {
		t.Error("a bad secret isn't an error")
	}
}

func TestEnrolment(t *testing.T) {
	s := NewSecret()
	if len(s) != 32 || s == NewSecret() {
		t.Fatalf("secret %q: want 32 random base32 characters", s)
	}
	u := URL("Taskwell admin", "ann@x.com", s)
	if !strings.HasPrefix(u, "otpauth://totp/Taskwell%20admin:ann@x.com?") || !strings.Contains(u, "secret="+s) {
		t.Errorf("URL = %q", u)
	}
	codes, hashes := NewRecoveryCodes()
	if len(codes) != RecoveryCount || len(hashes) != RecoveryCount {
		t.Fatalf("got %d codes, %d hashes", len(codes), len(hashes))
	}
	if HashRecovery(strings.ToUpper(" "+strings.ReplaceAll(codes[0], "-", " ")+" ")) != hashes[0] {
		t.Error("recovery codes don't match ignoring case, spaces and dashes")
	}
	if HashRecovery(codes[0]) == hashes[1] {
		t.Error("two recovery codes hash the same")
	}
}

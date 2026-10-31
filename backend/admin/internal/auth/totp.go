package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP per RFC 6238 with the authenticator-app defaults: HMAC-SHA1, 30-second steps, 6 digits.
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	totpSkew   = 1 // steps either side of now that still count, for clock drift
	totpIssuer = "Tether admin"

	recoveryCount = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a 160-bit key in base32, as authenticator apps take it.
func newTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return b32.EncodeToString(b)
}

// totpCode returns the code for the step (RFC 4226 HOTP with the step as counter).
func totpCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}
	mac := hmac.New(sha1.New, key)
	_ = binary.Write(mac, binary.BigEndian, step) // hash writes never fail
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000), nil
}

// checkTOTP returns the step that code matches, within totpSkew of now and after lastStep (so a used code can't be
// replayed); ok is false when none does.
func checkTOTP(secret, code string, now time.Time, lastStep int64) (step int64, ok bool, err error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false, nil
	}
	cur := now.Unix() / int64(totpStep.Seconds())
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		want, err := totpCode(secret, s)
		if err != nil {
			return 0, false, err
		}
		if s > lastStep && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true, nil
		}
	}
	return 0, false, nil
}

// otpauthURL is the key-URI authenticator apps import (typed or pasted; there is no QR code).
func otpauthURL(email, secret string) string {
	v := url.Values{"secret": {secret}, "issuer": {totpIssuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(totpIssuer+":"+email) + "?" + v.Encode()
}

// newRecoveryCodes returns recoveryCount single-use codes like "k3f9-x2mq-7hza" and their hashes to store.
func newRecoveryCodes() (codes, hashes []string) {
	for range recoveryCount {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		s := strings.ToLower(b32.EncodeToString(b))[:12]
		codes = append(codes, s[:4]+"-"+s[4:8]+"-"+s[8:])
		hashes = append(hashes, hashRecovery(s))
	}
	return codes, hashes
}

// hashRecovery hashes a recovery code as typed, ignoring case, spaces and dashes. The codes carry 60 random bits, so
// a fast hash is enough.
func hashRecovery(code string) string {
	c := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:])
}

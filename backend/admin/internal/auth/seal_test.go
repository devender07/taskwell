package auth

import "testing"

func TestSeal(t *testing.T) {
	key := MFAKey("a test key that is at least 32 bytes long")
	sealed, err := seal(key, "user-1", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := unseal(key, "user-1", sealed); err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if again, _ := seal(key, "user-1", "JBSWY3DPEHPK3PXP"); again == sealed {
		t.Error("sealing twice gave the same ciphertext (nonce reused)")
	}
	if _, err := unseal(key, "user-2", sealed); err == nil {
		t.Error("another user's row opened it")
	}
	if _, err := unseal(MFAKey("another key"), "user-1", sealed); err == nil {
		t.Error("a wrong key opened it")
	}
	if _, err := unseal(key, "user-1", "JBSWY3DPEHPK3PXP"); err == nil {
		t.Error("plaintext accepted as sealed")
	}
}

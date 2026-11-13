package aitest

import "testing"

func TestSeal(t *testing.T) {
	key, err := Key("0123456789abcdef0123456789abcdef", true)
	if err != nil {
		t.Fatal(err)
	}
	s, err := seal(key, "ws1", "acc1", "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := unseal(key, "ws1", "acc1", s); err != nil || got != "hunter2" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	for _, c := range []struct{ ws, acc string }{{"ws2", "acc1"}, {"ws1", "acc2"}} {
		if _, err := unseal(key, c.ws, c.acc, s); err == nil {
			t.Errorf("%s/%s opened another row's password", c.ws, c.acc)
		}
	}
	other, _ := Key("another secret of at least thirty-two bytes", true)
	if _, err := unseal(other, "ws1", "acc1", s); err == nil {
		t.Error("opened with the wrong key")
	}
	if _, err := Key("short", true); err == nil {
		t.Error("short production key accepted")
	}
}

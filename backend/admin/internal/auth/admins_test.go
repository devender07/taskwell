package auth

import (
	"strings"
	"testing"
)

func TestCheckNewAdmin(t *testing.T) {
	if errs := checkNewAdmin("Ada", "ada@example.com", "longenough", "mine"); len(errs) != 0 {
		t.Fatalf("valid form: %v", errs)
	}
	for field, args := range map[string][4]string{
		"name":             {"", "ada@example.com", "longenough", "mine"},
		"email":            {"Ada", "Ada <ada@example.com>", "longenough", "mine"},
		"password":         {"Ada", "ada@example.com", strings.Repeat("x", 73), "mine"},
		"current_password": {"Ada", "ada@example.com", "longenough", ""},
	} {
		if errs := checkNewAdmin(args[0], args[1], args[2], args[3]); errs[field] == "" || len(errs) != 1 {
			t.Errorf("%s: got %v", field, errs)
		}
	}
}

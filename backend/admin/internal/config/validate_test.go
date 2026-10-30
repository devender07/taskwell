package config

import "testing"

func TestCheckValue(t *testing.T) {
	cases := []struct {
		key, value string
		ok         bool
	}{
		{"smtp_port", "1025", true},
		{"smtp_port", "0", false},
		{"smtp_port", "65536", false},
		{"smtp_port", "25.5", false},
		{"smtp_port", "", false},
		{"digest_hour_utc", "0", true},
		{"digest_hour_utc", "23", true},
		{"digest_hour_utc", "24", false},
		{"access_ttl_minutes", "15", true},
		{"access_ttl_minutes", "0", false},
		{"refresh_ttl_hours", "720", true},
		{"short_refresh_ttl_hours", "-1", false},
		{"invite_ttl_days", "7", true},
		{"invite_ttl_days", "abc", false},
		{"upload_max_mb", "10", true},
		{"upload_max_mb", "0", false},
		{"otp_ttl_minutes", "1000001", false},
		{"app_base_url", "https://tether.example", true},
		{"app_base_url", "http://localhost:5173", true},
		{"app_base_url", "ftp://tether.example", false},
		{"app_base_url", "tether.example", false},
		{"app_base_url", "", false},
		{"smtp_from", "Tether <no-reply@tether.local>", true},
		{"smtp_from", "no-reply@tether.local", true},
		{"smtp_from", "Tether", false},
		{"smtp_host", "", true},
		{"google_client_id", "anything", true},
	}
	for _, c := range cases {
		if got := checkValue(c.key, c.value); (got == "") != c.ok {
			t.Errorf("checkValue(%q, %q) = %q, want ok=%v", c.key, c.value, got, c.ok)
		}
	}
}

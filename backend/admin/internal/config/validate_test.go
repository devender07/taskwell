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
		{"s3_endpoint", "", true},
		{"s3_endpoint", "minio:9000", false},
		{"upload_max_mb", "0", false},
		{"otp_ttl_minutes", "1000001", false},
		{"app_base_url", "https://taskwell.example", true},
		{"app_base_url", "http://localhost:5173", true},
		{"app_base_url", "ftp://taskwell.example", false},
		{"app_base_url", "taskwell.example", false},
		{"app_base_url", "", false},
		{"smtp_from", "Taskwell <no-reply@taskwell.local>", true},
		{"smtp_from", "no-reply@taskwell.local", true},
		{"smtp_from", "Taskwell", false},
		{"smtp_host", "", true},
		{"google_client_id", "anything", true},
		{"feature_chat_plans", "free,starter,pro,advanced", true},
		{"feature_chat_plans", "pro", true},
		{"feature_chat_plans", "advanced, pro", true},
		{"feature_chat_plans", "", true},
		{"feature_chat_plans", "true", false},
		{"feature_chat_plans", "pro,enterprise", false},
		{"feature_chat_plans", "pro,", false},
		{"feature_chat", "true", true},
		{"feature_chat", "false", true},
		{"feature_chat", "pro", false},
	}
	for _, c := range cases {
		if got := checkValue(c.key, c.value); (got == "") != c.ok {
			t.Errorf("checkValue(%q, %q) = %q, want ok=%v", c.key, c.value, got, c.ok)
		}
	}
}

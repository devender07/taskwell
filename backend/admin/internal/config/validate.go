package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// checkValue returns an error message for a value that doesn't fit its key's format, or "". Keys without a
// known format accept anything.
func checkValue(key, v string) string {
	if lo, hi, ok := intRange(key); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return fmt.Sprintf("Enter a whole number from %d to %d.", lo, hi)
		}
		return ""
	}
	switch key {
	case "app_base_url":
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "Enter a URL starting with http:// or https://."
		}
		if u.Scheme != "https" && os.Getenv("APP_ENV") == "production" {
			return "Enter a URL starting with https://."
		}
	case "s3_endpoint":
		if v == "" {
			return ""
		}
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "Enter a URL starting with http:// or https://, or leave it empty for AWS."
		}
	case "smtp_from":
		if _, err := mail.ParseAddress(v); err != nil {
			return "Enter an email address, like Tether <no-reply@example.com>."
		}
	}
	return ""
}

// intRange reports whether key holds a whole number and its allowed range.
func intRange(key string) (lo, hi int, ok bool) {
	switch {
	case key == "smtp_port":
		return 1, 65535, true
	case key == "digest_hour_utc":
		return 0, 23, true
	case key == "upload_max_mb", key == "upload_quota_mb", strings.HasPrefix(key, "invite_daily_"), strings.Contains(key, "_ttl_"),
		strings.HasSuffix(key, "_minutes"), strings.HasSuffix(key, "_hours"), strings.HasSuffix(key, "_days"):
		return 1, 1_000_000, true // the cap keeps durations far from int32 overflow in make_interval
	}
	return 0, 0, false
}

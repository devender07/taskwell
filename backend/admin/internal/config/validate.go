package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"slices"
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
	switch {
	case strings.HasPrefix(key, "plan_") && strings.HasSuffix(key, "_ai_usd"):
		if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 || n > 10_000 {
			return "Enter an amount in USD from 0 to 10000, like 3 or 2.5."
		}
	case strings.HasPrefix(key, "feature_") && strings.HasSuffix(key, "_plans"):
		if !planList(v) {
			return "Use plan ids: free, starter, pro, advanced."
		}
	case strings.HasPrefix(key, "feature_") && v != "true" && v != "false":
		return "Use true or false."
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
			return "Enter an email address, like Taskwell <no-reply@example.com>."
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
	case strings.HasPrefix(key, "plan_") && !strings.HasSuffix(key, "_ai_usd"):
		return 0, 1_000_000, true // 0 is unlimited
	case key == "upload_max_mb", strings.HasPrefix(key, "invite_daily_"), strings.Contains(key, "_ttl_"),
		strings.HasSuffix(key, "_minutes"), strings.HasSuffix(key, "_hours"), strings.HasSuffix(key, "_days"):
		return 1, 1_000_000, true // the cap keeps durations far from int32 overflow in make_interval
	}
	return 0, 0, false
}

// planList reports whether v is a comma-separated list of the user API's plan ids (its package plan), or empty.
func planList(v string) bool {
	if v == "" {
		return true
	}
	for _, id := range strings.Split(v, ",") {
		if !slices.Contains([]string{"free", "starter", "pro", "advanced"}, strings.TrimSpace(id)) {
			return false
		}
	}
	return true
}

// Package env reads the services' settings from environment variables.
package env

import "os"

// Get is the variable's value, or def when it's unset or empty.
func Get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

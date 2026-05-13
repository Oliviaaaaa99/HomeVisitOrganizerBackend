// Package configx loads typed configuration from environment variables.
package configx

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// String reads an env var or returns def.
func String(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// StringFirst returns the first non-empty env var from keys, or def if none are
// set. Useful when the same setting has two accepted names — e.g. Tigris
// (Fly's managed object storage) sets BUCKET_NAME / AWS_ENDPOINT_URL_S3 while
// our LocalStack-era convention uses S3_BUCKET / AWS_ENDPOINT_URL.
func StringFirst(def string, keys ...string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return def
}

// MustString reads an env var or returns an error if missing.
func MustString(key string) (string, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", fmt.Errorf("required env var %q is not set", key)
	}
	return v, nil
}

// Int reads an env var as int or returns def.
func Int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Duration reads an env var as time.Duration or returns def.
func Duration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

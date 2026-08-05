package config

import (
	"errors"

	"os"

	"strings"
)

func defaultAPIURL(host string) string {
	switch {
	case host == "github.com":
		return "https://api.github.com"
	case strings.HasSuffix(host, ".ghe.com"):
		return "https://api." + host
	default:
		return "https://" + host + "/api/v3"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("must be a boolean value")
	}
}

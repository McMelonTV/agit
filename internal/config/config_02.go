package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func validateRestrictedFlags(fs *flag.FlagSet) error {
	restricted := map[string]struct{}{
		"app-id":                {},
		"private-key":           {},
		"host":                  {},
		"api-url":               {},
		"api-version":           {},
		"cache-dir":             {},
		"no-cache":              {},
		"refresh-before":        {},
		"http-timeout":          {},
		"git-name":              {},
		"git-email":             {},
		"git-authorship":        {},
		"override-git-identity": {},
	}

	var changed []string
	fs.Visit(func(f *flag.Flag) {
		if _, ok := restricted[f.Name]; ok {
			changed = append(changed, "--"+f.Name)
		}
	})
	if len(changed) == 0 {
		return nil
	}
	return fmt.Errorf("cannot override %s inside an active viagh credential session; start a new top-level viagh process instead", strings.Join(changed, ", "))
}

func EncodeSession(cfg Config) (string, error) {
	cfg.PrivateKeyPath = ""
	cfg.PrivateKeyPEM = ""
	cfg.PrivateKeyBase64 = ""
	cfg.SessionRestricted = true
	contents, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode runtime configuration: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeSession(encoded string) (Config, error) {
	contents, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", SessionConfigEnv, err)
	}
	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", SessionConfigEnv, err)
	}
	cfg.SessionRestricted = true
	return normalize(cfg)
}

func normalize(cfg Config) (Config, error) {
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.Owner = strings.TrimSpace(cfg.Owner)
	cfg.Repository = strings.TrimSpace(cfg.Repository)
	cfg.GitName = strings.TrimSpace(cfg.GitName)
	cfg.GitEmail = strings.TrimSpace(cfg.GitEmail)
	cfg.GitAuthorship = strings.ToLower(strings.TrimSpace(cfg.GitAuthorship))
	if cfg.GitAuthorship == "" {
		cfg.GitAuthorship = GitAuthorshipBot
	}
	switch cfg.GitAuthorship {
	case GitAuthorshipBot, GitAuthorshipConfigured, GitAuthorshipBoth:
	default:
		return Config{}, fmt.Errorf("git-authorship must be one of %q, %q, or %q", GitAuthorshipBot, GitAuthorshipConfigured, GitAuthorshipBoth)
	}
	if strings.ContainsAny(cfg.GitName, "\x00\r\n") {
		return Config{}, errors.New("git-name must not contain NUL or line breaks")
	}
	if strings.ContainsAny(cfg.GitName, "<>") {
		return Config{}, errors.New("git-name must not contain angle brackets")
	}
	if strings.ContainsAny(cfg.GitEmail, "\x00\r\n") {
		return Config{}, errors.New("git-email must not contain NUL or line breaks")
	}
	if strings.ContainsAny(cfg.GitEmail, "<> \t") {
		return Config{}, errors.New("git-email must not contain whitespace or angle brackets")
	}
	if (cfg.GitAuthorship == GitAuthorshipConfigured || cfg.GitAuthorship == GitAuthorshipBoth) && (cfg.GitName == "" || cfg.GitEmail == "") {
		cfg.GitAuthorship = GitAuthorshipBot
	}
	cfg.Host = normalizeHost(cfg.Host)
	if err := validateHost(cfg.Host); err != nil {
		return Config{}, err
	}
	cfg.APIURL = strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	if cfg.APIURL == "" {
		cfg.APIURL = defaultAPIURL(cfg.Host)
	}
	parsed, err := url.Parse(cfg.APIURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		if err == nil {
			err = errors.New("URL must be an absolute http or https URL without credentials, query, or fragment")
		}
		return Config{}, fmt.Errorf("invalid GitHub API URL %q: %w", cfg.APIURL, err)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return Config{}, fmt.Errorf("invalid GitHub API URL %q: insecure HTTP is only allowed for loopback hosts", cfg.APIURL)
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	if cfg.CacheDir == "" && !cfg.NoCache {
		base, err := os.UserCacheDir()
		if err != nil || base == "" {
			cfg.NoCache = true
		} else {
			cfg.CacheDir = filepath.Join(base, "viagh")
		}
	}
	if cfg.RefreshBefore < 0 {
		return Config{}, errors.New("refresh-before must not be negative")
	}
	if cfg.HTTPTimeout <= 0 {
		return Config{}, errors.New("http-timeout must be positive")
	}
	if cfg.InstallationID < 0 {
		return Config{}, errors.New("installation-id must not be negative")
	}
	return cfg, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func applySessionSelectionOverrides(cfg Config) (Config, error) {
	if value, ok := os.LookupEnv("VIAGH_INSTALLATION_ID"); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			cfg.InstallationID = 0
		} else {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return Config{}, errors.New("VIAGH_INSTALLATION_ID must be a positive integer")
			}
			cfg.InstallationID = id
		}
	}
	if value, ok := os.LookupEnv("VIAGH_OWNER"); ok {
		cfg.Owner = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv("VIAGH_REPOSITORY"); ok {
		cfg.Repository = strings.TrimSpace(value)
	} else if value, ok := os.LookupEnv("GH_REPO"); ok {
		cfg.Repository = strings.TrimSpace(value)
	}
	return normalize(cfg)
}

func (c Config) ValidateCredentials() error {
	if c.SessionRestricted {
		return errors.New("GitHub App credentials are held by the active viagh credential broker")
	}
	if c.AppID == "" {
		return errors.New("missing GitHub App ID; set VIAGH_APP_ID or --app-id")
	}
	if c.PrivateKeyPath == "" && c.PrivateKeyPEM == "" && c.PrivateKeyBase64 == "" {
		return errors.New("missing GitHub App private key; set VIAGH_PRIVATE_KEY, VIAGH_PRIVATE_KEY_PEM, or VIAGH_PRIVATE_KEY_BASE64")
	}
	return nil
}

func (c Config) CloudHost() bool {
	return c.Host == "github.com" || strings.HasSuffix(c.Host, ".ghe.com")
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "github.com"
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
			value = parsed.Host
		}
	}
	value = strings.TrimSuffix(value, "/")
	if host, port, err := net.SplitHostPort(value); err == nil && port == "443" {
		if strings.Contains(host, ":") {
			value = "[" + host + "]"
		} else {
			value = host
		}
	}
	return strings.ToLower(value)
}

func validateHost(host string) error {
	if host == "" || strings.ContainsAny(host, "/?#@") {
		return fmt.Errorf("invalid GitHub host %q", host)
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid GitHub host %q", host)
	}
	return nil
}

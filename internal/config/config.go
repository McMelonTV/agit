package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIVersion = "2026-03-10"
	SessionConfigEnv  = "GHAPP_SESSION_CONFIG"

	GitAuthorshipBot        = "bot"
	GitAuthorshipConfigured = "configured"
	GitAuthorshipBoth       = "both"
)

type Config struct {
	AppID               string        `json:"app_id,omitempty"`
	PrivateKeyPath      string        `json:"private_key_path,omitempty"`
	PrivateKeyPEM       string        `json:"private_key_pem,omitempty"`
	PrivateKeyBase64    string        `json:"-"`
	InstallationID      int64         `json:"installation_id,omitempty"`
	Owner               string        `json:"owner,omitempty"`
	Repository          string        `json:"repository,omitempty"`
	Host                string        `json:"host"`
	APIURL              string        `json:"api_url"`
	APIVersion          string        `json:"api_version,omitempty"`
	CacheDir            string        `json:"cache_dir,omitempty"`
	NoCache             bool          `json:"no_cache,omitempty"`
	RefreshBefore       time.Duration `json:"refresh_before"`
	HTTPTimeout         time.Duration `json:"http_timeout"`
	RealGH              string        `json:"real_gh,omitempty"`
	RealGit             string        `json:"real_git,omitempty"`
	GitName             string        `json:"git_name,omitempty"`
	GitEmail            string        `json:"git_email,omitempty"`
	GitAuthorship       string        `json:"git_authorship"`
	OverrideGitIdentity bool          `json:"override_git_identity"`
	SessionRestricted   bool          `json:"session_restricted,omitempty"`
}

func FromEnv() (Config, error) {
	if encoded := os.Getenv(SessionConfigEnv); encoded != "" {
		cfg, err := decodeSession(encoded)
		if err != nil {
			return Config{}, err
		}
		return applySessionSelectionOverrides(cfg)
	}

	cfg := Config{
		AppID:               firstNonEmpty(os.Getenv("GHAPP_APP_ID"), os.Getenv("GITHUB_APP_ID")),
		PrivateKeyPath:      os.Getenv("GHAPP_PRIVATE_KEY"),
		PrivateKeyPEM:       os.Getenv("GHAPP_PRIVATE_KEY_PEM"),
		Owner:               os.Getenv("GHAPP_OWNER"),
		Repository:          firstNonEmpty(os.Getenv("GHAPP_REPOSITORY"), os.Getenv("GH_REPO")),
		Host:                firstNonEmpty(os.Getenv("GHAPP_HOST"), os.Getenv("GH_HOST"), "github.com"),
		APIURL:              os.Getenv("GHAPP_API_URL"),
		APIVersion:          firstNonEmpty(os.Getenv("GHAPP_API_VERSION"), defaultAPIVersion),
		CacheDir:            os.Getenv("GHAPP_CACHE_DIR"),
		NoCache:             envBool("GHAPP_NO_CACHE"),
		RefreshBefore:       5 * time.Minute,
		HTTPTimeout:         30 * time.Second,
		RealGH:              os.Getenv("GHAPP_REAL_GH"),
		RealGit:             os.Getenv("GHAPP_REAL_GIT"),
		GitName:             os.Getenv("GHAPP_GIT_NAME"),
		GitEmail:            os.Getenv("GHAPP_GIT_EMAIL"),
		GitAuthorship:       firstNonEmpty(os.Getenv("GHAPP_GIT_AUTHORSHIP"), GitAuthorshipBot),
		OverrideGitIdentity: true,
	}
	if value, ok := os.LookupEnv("GHAPP_OVERRIDE_GIT_IDENTITY"); ok && strings.TrimSpace(value) != "" {
		enabled, err := parseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("GHAPP_OVERRIDE_GIT_IDENTITY: %w", err)
		}
		cfg.OverrideGitIdentity = enabled
	}

	if value := os.Getenv("GITHUB_APP_PRIVATE_KEY"); cfg.PrivateKeyPath == "" && cfg.PrivateKeyPEM == "" && value != "" {
		if strings.Contains(value, "BEGIN ") || strings.ContainsAny(value, "\r\n") {
			cfg.PrivateKeyPEM = value
		} else {
			cfg.PrivateKeyPath = value
		}
	}

	if value := os.Getenv("GHAPP_PRIVATE_KEY_BASE64"); cfg.PrivateKeyPEM == "" && value != "" {
		cfg.PrivateKeyBase64 = value
	}

	if value := firstNonEmpty(os.Getenv("GHAPP_INSTALLATION_ID"), os.Getenv("GITHUB_APP_INSTALLATION_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return Config{}, errors.New("GHAPP_INSTALLATION_ID must be a positive integer")
		}
		cfg.InstallationID = id
	}
	if value := os.Getenv("GHAPP_REFRESH_BEFORE"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return Config{}, errors.New("GHAPP_REFRESH_BEFORE must be a non-negative duration")
		}
		cfg.RefreshBefore = d
	}
	if value := os.Getenv("GHAPP_HTTP_TIMEOUT"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return Config{}, errors.New("GHAPP_HTTP_TIMEOUT must be a positive duration")
		}
		cfg.HTTPTimeout = d
	}

	return normalize(cfg)
}

func Parse(args []string) (Config, []string, error) {
	base, err := FromEnv()
	if err != nil {
		return Config{}, nil, err
	}
	cfg := base

	fs := flag.NewFlagSet("ghapp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.AppID, "app-id", cfg.AppID, "GitHub App ID or client ID used as the JWT issuer")
	fs.StringVar(&cfg.PrivateKeyPath, "private-key", cfg.PrivateKeyPath, "path to the GitHub App private key PEM")
	fs.Int64Var(&cfg.InstallationID, "installation-id", cfg.InstallationID, "GitHub App installation ID")
	fs.StringVar(&cfg.Owner, "owner", cfg.Owner, "organization or user installation owner")
	fs.StringVar(&cfg.Repository, "repository", cfg.Repository, "repository selector in OWNER/REPO form")
	fs.StringVar(&cfg.Host, "host", cfg.Host, "GitHub hostname")
	fs.StringVar(&cfg.APIURL, "api-url", cfg.APIURL, "GitHub REST API base URL")
	fs.StringVar(&cfg.APIVersion, "api-version", cfg.APIVersion, "GitHub REST API version")
	fs.StringVar(&cfg.CacheDir, "cache-dir", cfg.CacheDir, "token cache directory")
	fs.BoolVar(&cfg.NoCache, "no-cache", cfg.NoCache, "disable installation token caching")
	fs.DurationVar(&cfg.RefreshBefore, "refresh-before", cfg.RefreshBefore, "refresh cached tokens this long before expiry")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", cfg.HTTPTimeout, "GitHub API request timeout")
	fs.StringVar(&cfg.RealGH, "real-gh", cfg.RealGH, "path to the underlying gh executable")
	fs.StringVar(&cfg.RealGit, "real-git", cfg.RealGit, "path to the underlying Git executable")
	fs.StringVar(&cfg.GitName, "git-name", cfg.GitName, "Git author name used for configured or both authorship")
	fs.StringVar(&cfg.GitEmail, "git-email", cfg.GitEmail, "Git author email used for configured or both authorship")
	fs.StringVar(&cfg.GitAuthorship, "git-authorship", cfg.GitAuthorship, "Git authorship mode: bot, configured, or both")
	fs.BoolVar(&cfg.OverrideGitIdentity, "override-git-identity", cfg.OverrideGitIdentity, "override inherited Git author and committer identity")

	if err := fs.Parse(args); err != nil {
		return Config{}, nil, err
	}
	if base.SessionRestricted {
		if err := validateRestrictedFlags(fs); err != nil {
			return Config{}, nil, err
		}
	}
	cfg, err = normalize(cfg)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, fs.Args(), nil
}

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
	return fmt.Errorf("cannot override %s inside an active ghapp credential session; start a new top-level ghapp process instead", strings.Join(changed, ", "))
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
			cfg.CacheDir = filepath.Join(base, "ghapp")
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
	if value, ok := os.LookupEnv("GHAPP_INSTALLATION_ID"); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			cfg.InstallationID = 0
		} else {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return Config{}, errors.New("GHAPP_INSTALLATION_ID must be a positive integer")
			}
			cfg.InstallationID = id
		}
	}
	if value, ok := os.LookupEnv("GHAPP_OWNER"); ok {
		cfg.Owner = strings.TrimSpace(value)
	}
	if value, ok := os.LookupEnv("GHAPP_REPOSITORY"); ok {
		cfg.Repository = strings.TrimSpace(value)
	} else if value, ok := os.LookupEnv("GH_REPO"); ok {
		cfg.Repository = strings.TrimSpace(value)
	}
	return normalize(cfg)
}

func (c Config) ValidateCredentials() error {
	if c.SessionRestricted {
		return errors.New("GitHub App credentials are held by the active ghapp credential broker")
	}
	if c.AppID == "" {
		return errors.New("missing GitHub App ID; set GHAPP_APP_ID or --app-id")
	}
	if c.PrivateKeyPath == "" && c.PrivateKeyPEM == "" && c.PrivateKeyBase64 == "" {
		return errors.New("missing GitHub App private key; set GHAPP_PRIVATE_KEY, GHAPP_PRIVATE_KEY_PEM, or GHAPP_PRIVATE_KEY_BASE64")
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

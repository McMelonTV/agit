package config

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"os"

	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIVersion = "2026-03-10"
	SessionConfigEnv  = "VIAGH_SESSION_CONFIG"

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
		AppID:               firstNonEmpty(os.Getenv("VIAGH_APP_ID"), os.Getenv("GITHUB_APP_ID")),
		PrivateKeyPath:      os.Getenv("VIAGH_PRIVATE_KEY"),
		PrivateKeyPEM:       os.Getenv("VIAGH_PRIVATE_KEY_PEM"),
		Owner:               os.Getenv("VIAGH_OWNER"),
		Repository:          firstNonEmpty(os.Getenv("VIAGH_REPOSITORY"), os.Getenv("GH_REPO")),
		Host:                firstNonEmpty(os.Getenv("VIAGH_HOST"), os.Getenv("GH_HOST"), "github.com"),
		APIURL:              os.Getenv("VIAGH_API_URL"),
		APIVersion:          firstNonEmpty(os.Getenv("VIAGH_API_VERSION"), defaultAPIVersion),
		CacheDir:            os.Getenv("VIAGH_CACHE_DIR"),
		NoCache:             envBool("VIAGH_NO_CACHE"),
		RefreshBefore:       5 * time.Minute,
		HTTPTimeout:         30 * time.Second,
		RealGH:              os.Getenv("VIAGH_REAL_GH"),
		RealGit:             os.Getenv("VIAGH_REAL_GIT"),
		GitName:             os.Getenv("VIAGH_GIT_NAME"),
		GitEmail:            os.Getenv("VIAGH_GIT_EMAIL"),
		GitAuthorship:       firstNonEmpty(os.Getenv("VIAGH_GIT_AUTHORSHIP"), GitAuthorshipBot),
		OverrideGitIdentity: true,
	}
	if value, ok := os.LookupEnv("VIAGH_OVERRIDE_GIT_IDENTITY"); ok && strings.TrimSpace(value) != "" {
		enabled, err := parseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("VIAGH_OVERRIDE_GIT_IDENTITY: %w", err)
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

	if value := os.Getenv("VIAGH_PRIVATE_KEY_BASE64"); cfg.PrivateKeyPEM == "" && value != "" {
		cfg.PrivateKeyBase64 = value
	}

	if value := firstNonEmpty(os.Getenv("VIAGH_INSTALLATION_ID"), os.Getenv("GITHUB_APP_INSTALLATION_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return Config{}, errors.New("VIAGH_INSTALLATION_ID must be a positive integer")
		}
		cfg.InstallationID = id
	}
	if value := os.Getenv("VIAGH_REFRESH_BEFORE"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return Config{}, errors.New("VIAGH_REFRESH_BEFORE must be a non-negative duration")
		}
		cfg.RefreshBefore = d
	}
	if value := os.Getenv("VIAGH_HTTP_TIMEOUT"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return Config{}, errors.New("VIAGH_HTTP_TIMEOUT must be a positive duration")
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

	fs := flag.NewFlagSet("viagh", flag.ContinueOnError)
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

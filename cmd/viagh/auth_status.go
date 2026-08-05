package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/McMelonTV/viagh/internal/config"
)

type ghAuthStatusEntry struct {
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
	Active      bool   `json:"active"`
	Host        string `json:"host"`
	Login       string `json:"login"`
	TokenSource string `json:"tokenSource"`
	GitProtocol string `json:"gitProtocol"`
}

type ghAuthStatusOutput struct {
	Hosts map[string][]ghAuthStatusEntry `json:"hosts"`
}

// runGitHubAppAuthStatus implements the machine-readable `gh auth status
// --json hosts` probe used by T3 Code and similar clients. It validates the App
// JWT and reports the App bot as the active account without selecting an
// installation or minting an installation token.
func runGitHubAppAuthStatus(cfg config.Config, host string) int {
	host = normalizeAuthStatusHost(cfg, host)
	login, authErr := probeAppBotIdentity(cfg)
	return writeGitHubAppAuthStatus(cfg, host, login, authErr)
}

// runGitHubAppAuthStatusText implements the human-readable `gh auth status`
// report. It mirrors GitHub CLI's plain output format after validating the App
// JWT, so interactive users see the App bot as the active account even though
// viagh keeps no credentials in the real GitHub CLI store.
func runGitHubAppAuthStatusText(cfg config.Config, host string) int {
	host = normalizeAuthStatusHost(cfg, host)
	login, authErr := probeAppBotIdentity(cfg)
	return writeGitHubAppAuthStatusText(cfg, host, login, authErr)
}

// probeAppBotIdentity validates the App JWT and returns the App bot login
// without selecting an installation or minting an installation token.
func probeAppBotIdentity(cfg config.Config) (string, error) {
	session, err := beginAuthSession(cfg)
	if err != nil {
		return "", err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	identity, identityErr := session.backend.BotIdentity(ctx)
	cancel()
	if identityErr != nil {
		return "", identityErr
	}
	login := strings.TrimSpace(identity.Name)
	if login == "" {
		return "", errors.New("GitHub returned an empty App bot login")
	}
	return login, nil
}

func normalizeAuthStatusHost(cfg config.Config, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(cfg.Host))
	}
	return host
}

func writeGitHubAppAuthStatus(cfg config.Config, host, login string, authErr error) int {
	if host == "" {
		host = "github.com"
	}
	entry := ghAuthStatusEntry{
		State:       "success",
		Active:      true,
		Host:        host,
		Login:       login,
		TokenSource: "viagh",
		GitProtocol: "https",
	}
	if authErr != nil {
		entry.State = "error"
		entry.Error = "GitHub App authentication failed: " + strings.TrimSpace(authErr.Error())
		entry.Login = fallbackAppStatusLogin(cfg)
	}
	output := ghAuthStatusOutput{Hosts: map[string][]ghAuthStatusEntry{host: {entry}}}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		return fail(err)
	}
	// gh auth status intentionally exits zero in JSON mode even when an account
	// is invalid. Consumers inspect each entry's state instead.
	return 0
}

func writeGitHubAppAuthStatusText(cfg config.Config, host, login string, authErr error) int {
	if host == "" {
		host = "github.com"
	}
	stream := os.Stdout
	code := 0
	if authErr != nil {
		stream = os.Stderr
		code = 1
	}
	fmt.Fprintf(stream, "%s\n", host)
	if authErr != nil {
		fmt.Fprintf(stream, "  X Failed to log in to %s account %s (viagh)\n", host, fallbackAppStatusLogin(cfg))
		fmt.Fprintf(stream, "  - Active account: true\n")
		fmt.Fprintf(stream, "  - GitHub App authentication failed: %s\n", strings.TrimSpace(authErr.Error()))
		return code
	}
	fmt.Fprintf(stream, "  \u2713 Logged in to %s account %s (viagh)\n", host, login)
	fmt.Fprintf(stream, "  - Active account: true\n")
	fmt.Fprintf(stream, "  - Git operations protocol: https\n")
	return 0
}

func fallbackAppStatusLogin(cfg config.Config) string {
	appID := strings.TrimSpace(cfg.AppID)
	if appID == "" {
		return "github-app"
	}
	return "github-app-" + appID
}

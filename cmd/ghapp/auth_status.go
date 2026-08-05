package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/McMelonTV/agit/internal/config"
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
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(cfg.Host))
	}

	session, err := beginAuthSession(cfg)
	if err != nil {
		return writeGitHubAppAuthStatus(cfg, host, "", err)
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	identity, identityErr := session.backend.BotIdentity(ctx)
	cancel()
	if identityErr != nil {
		return writeGitHubAppAuthStatus(cfg, host, "", identityErr)
	}
	login := strings.TrimSpace(identity.Name)
	if login == "" {
		return writeGitHubAppAuthStatus(cfg, host, "", errors.New("GitHub returned an empty App bot login"))
	}
	return writeGitHubAppAuthStatus(cfg, host, login, nil)
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
		TokenSource: "ghapp",
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

func fallbackAppStatusLogin(cfg config.Config) string {
	appID := strings.TrimSpace(cfg.AppID)
	if appID == "" {
		return "github-app"
	}
	return "github-app-" + appID
}

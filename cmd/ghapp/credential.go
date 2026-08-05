package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/McMelonTV/agit/internal/broker"
	"github.com/McMelonTV/agit/internal/config"
	"github.com/McMelonTV/agit/internal/repository"
)

type gitCredential struct {
	Protocol string
	Host     string
	Path     string
}

func runCredentialHelper(cfg config.Config, args []string) int {
	if len(args) != 1 {
		return fail(fmt.Errorf("credential-helper requires one operation"))
	}
	operation := args[0]
	if operation == "store" || operation == "erase" {
		return 0
	}
	if operation != "get" {
		// Git's credential-helper protocol requires helpers to silently ignore
		// operations they do not understand so newer Git versions can negotiate
		// capabilities without breaking older helpers.
		return 0
	}
	credential, err := readCredential(os.Stdin)
	if err != nil {
		return fail(err)
	}
	if !credentialMatches(credential, cfg.Host) {
		return 0
	}
	ref, ok := repository.FromCredentialPath(credential.Host, credential.Path, cfg.Host)
	if !ok && cfg.Repository != "" {
		if configured, parsed := repository.Parse(cfg.Repository); parsed && repository.HostMatches(configured.Host, cfg.Host) {
			ref, ok = configured, true
		}
	}
	if !ok && cfg.InstallationID == 0 && cfg.Owner == "" {
		return credentialFailure(fmt.Errorf("Git requested credentials for https://%s/%s, but no OWNER/REPO could be determined", credential.Host, credential.Path))
	}
	client, available := broker.FromEnv()
	if !available {
		return credentialFailure(fmt.Errorf("credential broker is unavailable"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	defer cancel()
	token, _, err := resolveWithBackend(ctx, client, cfg, ref, "")
	if err != nil {
		return credentialFailure(err)
	}
	fmt.Println("username=x-access-token")
	fmt.Println("password=" + token.Token)
	if !token.ExpiresAt.IsZero() {
		fmt.Printf("password_expiry_utc=%d\n", token.ExpiresAt.Unix())
	}
	fmt.Println()
	return 0
}

func credentialFailure(err error) int {
	// quit=1 tells Git not to continue to another helper or an interactive
	// prompt. Without it, a broker or routing failure could unexpectedly fall
	// through to GIT_ASKPASS, a terminal prompt, or a user credential helper.
	fmt.Println("quit=1")
	fmt.Println()
	fmt.Fprintln(os.Stderr, "ghapp:", err)
	return 0
}

func readCredential(reader io.Reader) (gitCredential, error) {
	var credential gitCredential
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "protocol":
			credential.Protocol = value
		case "host":
			credential.Host = value
		case "path":
			credential.Path = value
		case "url":
			parsed, err := url.Parse(value)
			if err == nil {
				credential.Protocol = parsed.Scheme
				credential.Host = parsed.Host
				credential.Path = strings.TrimPrefix(parsed.Path, "/")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return gitCredential{}, fmt.Errorf("read Git credential request: %w", err)
	}
	return credential, nil
}

func credentialMatches(credential gitCredential, configuredHost string) bool {
	if !strings.EqualFold(credential.Protocol, "https") {
		return false
	}
	return equalCredentialHost(credential.Host, configuredHost)
}

func equalCredentialHost(actual, expected string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if actual == expected {
		return true
	}
	actualHost, actualPort, actualErr := net.SplitHostPort(actual)
	expectedHost, expectedPort, expectedErr := net.SplitHostPort(expected)
	if actualErr != nil {
		actualHost = actual
		actualPort = ""
	}
	if expectedErr != nil {
		expectedHost = expected
		expectedPort = ""
	}
	if !strings.EqualFold(actualHost, expectedHost) {
		return false
	}
	return normalizedHTTPSPort(actualPort) == normalizedHTTPSPort(expectedPort)
}

func normalizedHTTPSPort(port string) string {
	if port == "" || port == "443" {
		return "443"
	}
	return port
}

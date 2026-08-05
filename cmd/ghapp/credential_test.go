package main

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McMelonTV/agit/internal/broker"
	"github.com/McMelonTV/agit/internal/config"
	"github.com/McMelonTV/agit/internal/githubapp"
	"github.com/McMelonTV/agit/internal/repository"
)

type recordingBackend struct {
	mu              sync.Mutex
	refs            []repository.Ref
	owners          []string
	installationIDs []int64
}

func (b *recordingBackend) ResolveToken(_ context.Context, ref repository.Ref, owner string) (githubapp.Token, githubapp.Installation, error) {
	b.mu.Lock()
	b.refs = append(b.refs, ref)
	b.owners = append(b.owners, owner)
	b.mu.Unlock()
	return githubapp.Token{Token: "token-for-" + ref.Owner, ExpiresAt: time.Unix(2_000_000_000, 0)}, githubapp.Installation{ID: 7, Account: githubapp.Account{Login: ref.Owner}}, nil
}

func (b *recordingBackend) Installations(context.Context) ([]githubapp.Installation, error) {
	return []githubapp.Installation{{ID: 7, Account: githubapp.Account{Login: "acme"}}}, nil
}

func (b *recordingBackend) Installation(_ context.Context, id int64) (githubapp.Installation, error) {
	b.mu.Lock()
	b.installationIDs = append(b.installationIDs, id)
	b.mu.Unlock()
	return githubapp.Installation{ID: id, Account: githubapp.Account{Login: "selected"}}, nil
}

func (b *recordingBackend) TokenForInstallation(_ context.Context, id int64) (githubapp.Token, error) {
	b.mu.Lock()
	b.installationIDs = append(b.installationIDs, id)
	b.mu.Unlock()
	return githubapp.Token{Token: "token-by-id", ExpiresAt: time.Unix(2_000_000_000, 0)}, nil
}

func TestCredentialHelperRejectsUnrelatedHost(t *testing.T) {
	backend := &recordingBackend{}
	server, err := broker.Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv(broker.URLEnv, server.URL)
	t.Setenv(broker.SecretEnv, server.Secret)
	t.Setenv("GHAPP_RUNTIME_TOKEN", "stale-secret")

	cfg := config.Config{Host: "github.com", HTTPTimeout: time.Second}
	stdout, stderr, code := runCredentialWithInput(t, cfg, "protocol=https\nhost=attacker.example\npath=acme/widgets.git\n\n")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.refs) != 0 || len(backend.installationIDs) != 0 {
		t.Fatalf("broker was called for unrelated host: %+v %+v", backend.refs, backend.installationIDs)
	}
}

func TestCredentialHelperResolvesRepositoryFromCredentialProtocol(t *testing.T) {
	backend := &recordingBackend{}
	server, err := broker.Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv(broker.URLEnv, server.URL)
	t.Setenv(broker.SecretEnv, server.Secret)

	cfg := config.Config{Host: "github.com", HTTPTimeout: time.Second}
	stdout, stderr, code := runCredentialWithInput(t, cfg, "protocol=https\nhost=github.com:443\npath=acme/widgets.git\n\n")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "username=x-access-token\n") || !strings.Contains(stdout, "password=token-for-acme\n") {
		t.Fatalf("unexpected credential output: %q", stdout)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.refs) != 1 || backend.refs[0].String() != "acme/widgets" {
		t.Fatalf("resolved refs: %+v", backend.refs)
	}
}

func TestCredentialHelperHonorsInstallationIDFromSessionConfig(t *testing.T) {
	backend := &recordingBackend{}
	server, err := broker.Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv(broker.URLEnv, server.URL)
	t.Setenv(broker.SecretEnv, server.Secret)

	cfg := config.Config{Host: "github.com", InstallationID: 99, HTTPTimeout: time.Second}
	stdout, stderr, code := runCredentialWithInput(t, cfg, "protocol=https\nhost=github.com\n\n")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "password=token-by-id") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.installationIDs) != 2 || backend.installationIDs[0] != 99 || backend.installationIDs[1] != 99 {
		t.Fatalf("installation calls: %v", backend.installationIDs)
	}
}

func TestCredentialHelperDoesNotUseDifferentHostRepositoryFallback(t *testing.T) {
	backend := &recordingBackend{}
	server, err := broker.Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv(broker.URLEnv, server.URL)
	t.Setenv(broker.SecretEnv, server.Secret)
	cfg := config.Config{Host: "github.com", Repository: "github.other.example/acme/widgets", HTTPTimeout: time.Second}
	stdout, stderr, code := runCredentialWithInput(t, cfg, "protocol=https\nhost=github.com\n\n")
	if code != 0 || stdout != "quit=1\n\n" || !strings.Contains(stderr, "no OWNER/REPO could be determined") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.refs) != 0 {
		t.Fatalf("broker was called with different-host fallback: %+v", backend.refs)
	}
}

func TestCredentialHelperStopsFallbackWhenBrokerUnavailable(t *testing.T) {
	t.Setenv(broker.URLEnv, "")
	t.Setenv(broker.SecretEnv, "")
	cfg := config.Config{Host: "github.com", HTTPTimeout: time.Second}
	stdout, stderr, code := runCredentialWithInput(t, cfg, "protocol=https\nhost=github.com\npath=acme/widgets.git\n\n")
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "quit=1\n\n" {
		t.Fatalf("stdout=%q", stdout)
	}
	if !strings.Contains(stderr, "credential broker is unavailable") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCredentialHelperIgnoresUnknownOperation(t *testing.T) {
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runCredentialHelper(config.Config{Host: "github.com"}, []string{"capability"})
	})
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestCredentialHelperConfigurationErrorStopsGitFallback(t *testing.T) {
	t.Setenv(config.SessionConfigEnv, "not-valid-base64")
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{"git", "credential-helper", "get"})
	})
	if code != 0 || stdout != "quit=1\n\n" || !strings.Contains(stderr, config.SessionConfigEnv) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func runCredentialWithInput(t *testing.T, cfg config.Config, input string) (stdout, stderr string, code int) {
	t.Helper()
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, input); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	os.Stdin = reader
	defer func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	}()
	return captureProcessOutput(t, func() int {
		return runCredentialHelper(cfg, []string{"get"})
	})
}

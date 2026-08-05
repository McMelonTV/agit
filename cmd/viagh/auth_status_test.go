package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type t3CodeAuthStatus struct {
	Hosts map[string][]struct {
		State       string `json:"state"`
		Error       string `json:"error"`
		Active      bool   `json:"active"`
		Host        string `json:"host"`
		Login       string `json:"login"`
		TokenSource string `json:"tokenSource"`
		GitProtocol string `json:"gitProtocol"`
	} `json:"hosts"`
}

func TestT3CodeAuthProbeReportsAppBotWithoutSelectingInstallation(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "test-app"})
		case r.Method == http.MethodGet && r.URL.Path == "/users/test-app[bot]":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12345, "login": "test-app[bot]"})
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()

	// If the compatibility path accidentally invokes the underlying gh binary,
	// the test process exits 99 through TestMain.
	t.Setenv("VIAGH_TEST_FAKE_GH", "1")
	fakeGH := writeFakeGHCopy(t)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{
			"viagh",
			"--app-id", "123",
			"--private-key", keyPath,
			"--api-url", server.URL,
			"--real-gh", fakeGH,
			"--no-cache",
			"gh", "auth", "status", "--json", "hosts",
		})
	})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var status t3CodeAuthStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode auth status %q: %v", stdout, err)
	}
	entries := status.Hosts["github.com"]
	if len(entries) != 1 {
		t.Fatalf("auth entries = %#v", status.Hosts)
	}
	entry := entries[0]
	if entry.State != "success" || !entry.Active || entry.Host != "github.com" || entry.Login != "test-app[bot]" {
		t.Fatalf("unexpected T3 Code auth entry: %#v", entry)
	}
	if entry.TokenSource != "viagh" || entry.GitProtocol != "https" || entry.Error != "" {
		t.Fatalf("unexpected compatibility fields: %#v", entry)
	}

	mu.Lock()
	gotRequests := strings.Join(requests, ",")
	mu.Unlock()
	if gotRequests != "GET /app,GET /users/test-app[bot]" {
		t.Fatalf("auth probe made unexpected API requests: %s", gotRequests)
	}
	if strings.Contains(gotRequests, "installation") {
		t.Fatalf("auth probe selected or minted an installation: %s", gotRequests)
	}
}

func TestT3CodeAuthProbeReturnsParseableErrorWithZeroExitCode(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	t.Setenv("VIAGH_TEST_FAKE_GH", "1")
	fakeGH := writeFakeGHCopy(t)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return run([]string{
			"viagh",
			"--app-id", "123",
			"--private-key", keyPath,
			"--api-url", server.URL,
			"--real-gh", fakeGH,
			"--no-cache",
			"gh", "auth", "status", "--json=hosts",
		})
	})
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var status t3CodeAuthStatus
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode auth status %q: %v", stdout, err)
	}
	entries := status.Hosts["github.com"]
	if len(entries) != 1 || entries[0].State != "error" || !entries[0].Active {
		t.Fatalf("unexpected error status: %#v", status.Hosts)
	}
	if entries[0].Login != "github-app-123" || !strings.Contains(entries[0].Error, "Bad credentials") {
		t.Fatalf("unexpected error entry: %#v", entries[0])
	}
}

func writeFakeGHCopy(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

package main

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"os"

	"strings"

	"testing"

	"github.com/McMelonTV/viagh/internal/githubapp"
)

func TestRepoListFormatterAcrossInstallationsFailsExplicitly(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/app/installations" {
			_ = json.NewEncoder(w).Encode([]githubapp.Installation{
				{ID: 1, Account: githubapp.Account{Login: "alpha"}},
				{ID: 2, Account: githubapp.Account{Login: "beta"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	cfg := testConfig(keyPath, server.URL)
	stdout, stderr, code := captureProcessOutput(t, func() int {
		return runRepoListAcrossInstallations(cfg, &localBackend{cfg: cfg}, "/missing/gh", []string{"repo", "list", "--json", "name", "-q.length"}, "/missing/viagh", os.Environ())
	})
	if code != 1 || stdout != "" || !strings.Contains(stderr, "cannot apply --jq or --template") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

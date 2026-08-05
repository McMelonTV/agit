package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRepositoryInstallationAndCachedToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer JWT")
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2026-03-10" {
			t.Errorf("API version = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/installation":
			_ = json.NewEncoder(w).Encode(Installation{ID: 42, Account: Account{Login: "acme", Type: "Organization"}})
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
			tokenRequests.Add(1)
			_ = json.NewEncoder(w).Encode(Token{Token: "ghs_test_token", ExpiresAt: time.Now().Add(time.Hour).UTC()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("123", key, server.URL, "2026-03-10", server.Client())
	installation, err := client.RepositoryInstallation(context.Background(), "acme", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if installation.ID != 42 || installation.Account.Login != "acme" {
		t.Fatalf("unexpected installation: %+v", installation)
	}

	cache := TokenCache{Dir: t.TempDir()}
	first, err := client.CachedInstallationToken(context.Background(), 42, cache, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.CachedInstallationToken(context.Background(), 42, cache, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token != "ghs_test_token" || second.Token != first.Token {
		t.Fatalf("unexpected cached tokens: %+v %+v", first, second)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token endpoint called %d times, want 1", got)
	}
}

func TestOwnerInstallationFallsBackToUser(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/octocat/installation":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case "/users/octocat/installation":
			_ = json.NewEncoder(w).Encode(Installation{ID: 99, Account: Account{Login: "octocat", Type: "User"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("123", key, server.URL, "2026-03-10", server.Client())
	installation, err := client.OwnerInstallation(context.Background(), "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if installation.ID != 99 {
		t.Fatalf("installation ID = %d, want 99", installation.ID)
	}
}

func TestInstallationAndPaginatedInstallations(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/7":
			_ = json.NewEncoder(w).Encode(Installation{ID: 7, Account: Account{Login: "acme", Type: "Organization"}})
		case "/app/installations":
			page := r.URL.Query().Get("page")
			if page == "1" {
				items := make([]Installation, 100)
				for index := range items {
					items[index] = Installation{ID: int64(index + 1), Account: Account{Login: "org" + strconv.Itoa(index+1)}}
				}
				_ = json.NewEncoder(w).Encode(items)
				return
			}
			_ = json.NewEncoder(w).Encode([]Installation{{ID: 101, Account: Account{Login: "last"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("123", key, server.URL, "2026-03-10", server.Client())
	installation, err := client.Installation(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if installation.Account.Login != "acme" {
		t.Fatalf("installation = %+v", installation)
	}

	installations, err := client.Installations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 101 || installations[100].Account.Login != "last" {
		t.Fatalf("unexpected installations: len=%d last=%+v", len(installations), installations[len(installations)-1])
	}
}

func TestCachedInstallationTokenSerializesConcurrentProcesses(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
			http.NotFound(w, r)
			return
		}
		tokenRequests.Add(1)
		time.Sleep(75 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(Token{Token: "shared-token", ExpiresAt: time.Now().Add(time.Hour).UTC()})
	}))
	defer server.Close()

	client := NewClient("123", key, server.URL, "2026-03-10", server.Client())
	cache := TokenCache{Dir: t.TempDir()}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			token, err := client.CachedInstallationToken(context.Background(), 42, cache, time.Minute)
			if err == nil && token.Token != "shared-token" {
				err = errors.New("unexpected token")
			}
			errCh <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token endpoint called %d times, want 1", got)
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client := NewClient("123", key, source.URL, "2026-03-10", source.Client())
	if _, err := client.Installations(context.Background()); err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("unexpected redirect error: %v", err)
	}
	if calls := targetCalls.Load(); calls != 0 {
		t.Fatalf("redirect target was called %d times", calls)
	}
}

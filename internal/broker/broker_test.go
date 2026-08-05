package broker

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/McMelonTV/agit/internal/githubapp"
	"github.com/McMelonTV/agit/internal/repository"
)

type testBackend struct {
	calls atomic.Int32
}

func (b *testBackend) ResolveToken(_ context.Context, ref repository.Ref, owner string) (githubapp.Token, githubapp.Installation, error) {
	b.calls.Add(1)
	login := owner
	if login == "" {
		login = ref.Owner
	}
	return githubapp.Token{Token: "token-" + login, ExpiresAt: time.Now().Add(time.Hour)}, githubapp.Installation{ID: 7, Account: githubapp.Account{Login: login}}, nil
}

func (b *testBackend) Installations(context.Context) ([]githubapp.Installation, error) {
	b.calls.Add(1)
	return []githubapp.Installation{{ID: 7, Account: githubapp.Account{Login: "acme"}}}, nil
}

func (b *testBackend) Installation(context.Context, int64) (githubapp.Installation, error) {
	b.calls.Add(1)
	return githubapp.Installation{ID: 7, Account: githubapp.Account{Login: "acme"}}, nil
}

func (b *testBackend) TokenForInstallation(context.Context, int64) (githubapp.Token, error) {
	b.calls.Add(1)
	return githubapp.Token{Token: "token-7", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func TestBrokerClientRoundTrip(t *testing.T) {
	backend := &testBackend{}
	server, err := Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := Client{URL: server.URL, Secret: server.Secret, HTTPClient: &http.Client{Timeout: time.Second}}
	token, installation, err := client.ResolveToken(context.Background(), repository.Ref{Owner: "acme", Repo: "widgets"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if token.Token != "token-acme" || installation.Account.Login != "acme" {
		t.Fatalf("unexpected response: %+v %+v", token, installation)
	}
}

func TestBrokerRejectsWrongSecretWithoutCallingBackend(t *testing.T) {
	backend := &testBackend{}
	server, err := Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := Client{URL: server.URL, Secret: "wrong", HTTPClient: &http.Client{Timeout: time.Second}}
	if _, _, err := client.ResolveToken(context.Background(), repository.Ref{Owner: "acme", Repo: "widgets"}, ""); err == nil {
		t.Fatal("expected authorization error")
	}
	if calls := backend.calls.Load(); calls != 0 {
		t.Fatalf("backend called %d times", calls)
	}
}

func TestFromEnvAcceptsOnlyLoopbackHTTPBroker(t *testing.T) {
	t.Setenv(SecretEnv, "secret")
	for _, value := range []string{"https://127.0.0.1:1234", "http://example.com:1234", "http://localhost:1234", "http://127.0.0.1", "http://127.0.0.1:bad", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234/path"} {
		t.Setenv(URLEnv, value)
		if _, ok := FromEnv(); ok {
			t.Fatalf("accepted broker URL %q", value)
		}
	}
	t.Setenv(URLEnv, "http://127.0.0.1:1234")
	if client, ok := FromEnv(); !ok || client.URL != "http://127.0.0.1:1234" {
		t.Fatalf("loopback broker not accepted: %+v, %v", client, ok)
	}
}

func TestBrokerRejectsTrailingJSON(t *testing.T) {
	backend := &testBackend{}
	server, err := Start(backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/token", strings.NewReader(`{"repository":{}} {}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+server.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if calls := backend.calls.Load(); calls != 0 {
		t.Fatalf("backend called %d times", calls)
	}
}

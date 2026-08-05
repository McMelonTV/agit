package opencode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sessionJSON(updated int64, provider, model, variant, version string) []byte {
	record := map[string]any{
		"model": map[string]any{
			"id":         model,
			"providerID": provider,
			"variant":    variant,
		},
		"version": version,
		"time":    map[string]any{"updated": updated},
	}
	contents, _ := json.Marshal([]any{record})
	return contents
}

func TestDiscoverTrailerFromServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Errorf("path = %q, want /session", r.URL.Path)
		}
		if r.URL.Query().Get("directory") != "/work/repo" {
			t.Errorf("directory = %q", r.URL.Query().Get("directory"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(sessionJSON(200, "opencode-go", "deepseek-v4-flash", "max", "1.18.13"))
	}))
	defer server.Close()

	trailer := DiscoverTrailerFromServer(server.URL, "/work/repo", 2*time.Second)
	want := "Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`."
	if trailer != want {
		t.Fatalf("trailer = %q, want %q", trailer, want)
	}
}

func TestDiscoverTrailerFromServerOmitsDefaultVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sessionJSON(200, "opencode-go", "deepseek-v4-flash", "default", "1.18.13"))
	}))
	defer server.Close()

	trailer := DiscoverTrailerFromServer(server.URL, "/work/repo", 2*time.Second)
	want := "Worked on by `opencode-go/deepseek-v4-flash` within `opencode 1.18.13`."
	if trailer != want {
		t.Fatalf("trailer = %q, want %q", trailer, want)
	}
}

func TestDiscoverTrailerFromServerPicksMostRecent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		older := sessionJSON(100, "opencode-go", "older-model", "low", "1.18.13")
		newer := sessionJSON(300, "opencode-go", "newer-model", "max", "1.19.0")
		merged := append(older[:len(older)-1], append([]byte(","), newer[1:]...)...)
		_, _ = w.Write(merged)
	}))
	defer server.Close()

	trailer := DiscoverTrailerFromServer(server.URL, "/work/repo", 2*time.Second)
	want := "Worked on by `opencode-go/newer-model:max` within `opencode 1.19.0`."
	if trailer != want {
		t.Fatalf("trailer = %q, want %q", trailer, want)
	}
}

func TestDiscoverTrailerFromServerUnreachable(t *testing.T) {
	trailer := DiscoverTrailerFromServer("http://127.0.0.1:1", "/work/repo", 100*time.Millisecond)
	if trailer != "" {
		t.Fatalf("trailer = %q, want empty", trailer)
	}
}

func TestDiscoverTrailerFromServerEmptySessions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	trailer := DiscoverTrailerFromServer(server.URL, "/work/repo", 2*time.Second)
	if trailer != "" {
		t.Fatalf("trailer = %q, want empty", trailer)
	}
}

func TestDiscoverTrailerFromServerMissingVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sessionJSON(200, "opencode-go", "deepseek-v4-flash", "", ""))
	}))
	defer server.Close()

	trailer := DiscoverTrailerFromServer(server.URL, "/work/repo", 2*time.Second)
	if trailer != "" {
		t.Fatalf("trailer = %q, want empty", trailer)
	}
}

func TestDiscoverTrailerRequiresMarkerAndPort(t *testing.T) {
	t.Setenv(markerEnv, "")
	t.Setenv(portEnv, "12345")
	if trailer := DiscoverTrailer("/work/repo", 2*time.Second); trailer != "" {
		t.Fatalf("trailer = %q without marker, want empty", trailer)
	}
	t.Setenv(markerEnv, "1")
	t.Setenv(portEnv, "")
	if trailer := DiscoverTrailer("/work/repo", 2*time.Second); trailer != "" {
		t.Fatalf("trailer = %q without port, want empty", trailer)
	}
}

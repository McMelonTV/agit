package opencode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DiscoverTimeout bounds the loopback server query used to build an opencode
// attribution trailer. Discovery is best-effort and must never block a commit.
const DiscoverTimeout = 2 * time.Second

// markerEnv and portEnv identify an opencode subprocess and its local server.
const (
	markerEnv = "OPENCODE"
	portEnv   = "OPENCODE_PORT"
)

// DiscoverTrailer queries the local opencode server for the session most
// recently active in dir and returns a commit trailer describing the model
// and opencode version, or "" when the trailer cannot be determined.
//
// The server port is read from the OPENCODE_PORT environment variable and the
// marker OPENCODE=1 confirms the caller is running as an opencode subprocess.
// Failure to reach the server or to find a matching session is treated as "no
// trailer" rather than as an error.
func DiscoverTrailer(dir string, timeout time.Duration) string {
	if _, ok := os.LookupEnv(markerEnv); !ok || strings.TrimSpace(os.Getenv(markerEnv)) == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv(portEnv))
	if port == "" {
		return ""
	}
	base := "http://127.0.0.1:" + port
	return DiscoverTrailerFromServer(base, dir, timeout)
}

// DiscoverTrailerFromServer is DiscoverTrailer against an explicit server
// base URL. It is exported for tests and for callers that know the server
// location without consulting the process environment.
func DiscoverTrailerFromServer(baseURL, dir string, timeout time.Duration) string {
	session, ok := currentSession(baseURL, dir, timeout)
	if !ok {
		return ""
	}
	model := modelString(session.Model.ProviderID, session.Model.ID, session.Model.Variant)
	version := strings.TrimSpace(session.Version)
	if model == "" || version == "" {
		return ""
	}
	return fmt.Sprintf("Worked on by `%s` within `opencode %s`.", model, version)
}

func currentSession(baseURL, dir string, timeout time.Duration) (sessionRecord, bool) {
	query := url.Values{}
	query.Set("directory", dir)
	endpoint := baseURL + "/session?" + query.Encode()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return sessionRecord{}, false
	}
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return sessionRecord{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return sessionRecord{}, false
	}
	var sessions []sessionRecord
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&sessions); err != nil {
		return sessionRecord{}, false
	}
	return mostRecent(sessions)
}

type sessionRecord struct {
	Model struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Variant    string `json:"variant"`
	} `json:"model"`
	Version string `json:"version"`
	Time    struct {
		Updated int64 `json:"updated"`
	} `json:"time"`
}

func mostRecent(sessions []sessionRecord) (sessionRecord, bool) {
	best, ok := sessionRecord{}, false
	for _, session := range sessions {
		if !ok || session.Time.Updated > best.Time.Updated {
			best, ok = session, true
		}
	}
	return best, ok
}

func modelString(providerID, id, variant string) string {
	model := strings.TrimSpace(providerID) + "/" + strings.TrimSpace(id)
	if model == "/" {
		return ""
	}
	variant = strings.TrimSpace(variant)
	if variant != "" && !strings.EqualFold(variant, "default") {
		model += ":" + variant
	}
	return model
}

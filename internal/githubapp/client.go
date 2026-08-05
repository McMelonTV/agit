package githubapp

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	AppID      string
	PrivateKey *rsa.PrivateKey
	APIURL     string
	APIVersion string
	HTTPClient *http.Client
	UserAgent  string
	Now        func() time.Time
}

type Installation struct {
	ID      int64   `json:"id"`
	Account Account `json:"account"`
}

type Account struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Token struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("GitHub API returned HTTP %d: %s", e.StatusCode, e.Message)
}

func IsStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

func NewClient(appID string, key *rsa.PrivateKey, apiURL, apiVersion string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		AppID:      appID,
		PrivateKey: key,
		APIURL:     strings.TrimRight(apiURL, "/"),
		APIVersion: apiVersion,
		HTTPClient: sameOriginRedirectClient(httpClient),
		UserAgent:  "viagh/dev",
		Now:        time.Now,
	}
}

func sameOriginRedirectClient(client *http.Client) *http.Client {
	clone := *client
	previous := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			return fmt.Errorf("refuse GitHub API redirect from %s to different origin %s", via[0].URL.Redacted(), req.URL.Redacted())
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 GitHub API redirects")
		}
		return nil
	}
	return &clone
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && equalURLHost(left, right)
}

func equalURLHost(left, right *url.URL) bool {
	if !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return normalizedPort(left) == normalizedPort(right)
}

func normalizedPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	if _, port, err := net.SplitHostPort(value.Host); err == nil {
		return port
	}
	return ""
}

func (c *Client) JWT() (string, error) {
	return GenerateJWT(c.AppID, c.PrivateKey, c.Now())
}

func (c *Client) RepositoryInstallation(ctx context.Context, owner, repo string) (Installation, error) {
	var installation Installation
	endpoint := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/installation"
	err := c.doAppRequest(ctx, http.MethodGet, endpoint, nil, &installation)
	return installation, err
}

func (c *Client) Installation(ctx context.Context, installationID int64) (Installation, error) {
	var installation Installation
	endpoint := "/app/installations/" + strconv.FormatInt(installationID, 10)
	err := c.doAppRequest(ctx, http.MethodGet, endpoint, nil, &installation)
	return installation, err
}

func (c *Client) OwnerInstallation(ctx context.Context, owner string) (Installation, error) {
	var installation Installation
	orgEndpoint := "/orgs/" + url.PathEscape(owner) + "/installation"
	if err := c.doAppRequest(ctx, http.MethodGet, orgEndpoint, nil, &installation); err == nil {
		return installation, nil
	} else if !IsStatus(err, http.StatusNotFound) {
		return Installation{}, err
	}

	userEndpoint := "/users/" + url.PathEscape(owner) + "/installation"
	if err := c.doAppRequest(ctx, http.MethodGet, userEndpoint, nil, &installation); err != nil {
		return Installation{}, err
	}
	return installation, nil
}

func (c *Client) Installations(ctx context.Context) ([]Installation, error) {
	var all []Installation
	for page := 1; ; page++ {
		var installations []Installation
		endpoint := "/app/installations?per_page=100&page=" + strconv.Itoa(page)
		if err := c.doAppRequest(ctx, http.MethodGet, endpoint, nil, &installations); err != nil {
			return nil, err
		}
		all = append(all, installations...)
		if len(installations) < 100 {
			return all, nil
		}
	}
}

func (c *Client) CreateInstallationToken(ctx context.Context, installationID int64) (Token, error) {
	var token Token
	endpoint := "/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	if err := c.doAppRequest(ctx, http.MethodPost, endpoint, map[string]any{}, &token); err != nil {
		return Token{}, err
	}
	if token.Token == "" || token.ExpiresAt.IsZero() {
		return Token{}, errors.New("GitHub returned an incomplete installation token response")
	}
	return token, nil
}

func (c *Client) BotIdentity(ctx context.Context) (Identity, error) {
	var app struct {
		Slug string `json:"slug"`
	}
	if err := c.doAppRequest(ctx, http.MethodGet, "/app", nil, &app); err != nil {
		return Identity{}, fmt.Errorf("get GitHub App identity: %w", err)
	}
	app.Slug = strings.TrimSpace(app.Slug)
	if app.Slug == "" {
		return Identity{}, errors.New("GitHub returned an App without a slug")
	}

	login := app.Slug + "[bot]"
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	endpoint := "/users/" + url.PathEscape(login)
	// The public user endpoint does not accept a GitHub App JWT as its
	// authentication mechanism. Query it without credentials so GitHub.com can
	// provide the bot account's numeric ID without minting an installation token.
	// Private GitHub Enterprise instances may reject the anonymous request; the
	// deterministic no-ID address below remains the compatibility fallback.
	if err := c.doPublicRequest(ctx, http.MethodGet, endpoint, &user); err == nil {
		if strings.TrimSpace(user.Login) != "" {
			login = strings.TrimSpace(user.Login)
		}
		if user.ID > 0 {
			return Identity{Name: login, Email: strconv.FormatInt(user.ID, 10) + "+" + login + "@" + c.noReplyDomain()}, nil
		}
	}

	// Some GitHub Enterprise versions do not expose the App bot through the
	// users endpoint. The legacy no-ID address still provides a stable,
	// deterministic bot identity, although an App rename can change attribution.
	return Identity{Name: login, Email: login + "@" + c.noReplyDomain()}, nil
}

func (c *Client) doPublicRequest(ctx context.Context, method, endpoint string, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.APIURL+endpoint, nil)
	if err != nil {
		return fmt.Errorf("create GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.APIVersion != "" {
		req.Header.Set("X-GitHub-Api-Version", c.APIVersion)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("call GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited := io.LimitReader(resp.Body, 64<<10)
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(limited).Decode(&payload)
		return &APIError{StatusCode: resp.StatusCode, Message: payload.Message}
	}
	if result == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}

func (c *Client) noReplyDomain() string {
	parsed, err := url.Parse(c.APIURL)
	if err != nil || parsed.Hostname() == "" {
		return "users.noreply.github.com"
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "api.github.com":
		return "users.noreply.github.com"
	case strings.HasPrefix(host, "api.") && strings.HasSuffix(host, ".ghe.com"):
		return "users.noreply." + strings.TrimPrefix(host, "api.")
	default:
		return "users.noreply." + host
	}
}

func (c *Client) doAppRequest(ctx context.Context, method, endpoint string, body any, result any) error {
	jwt, err := c.JWT()
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GitHub API request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.APIURL+endpoint, reader)
	if err != nil {
		return fmt.Errorf("create GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("User-Agent", c.UserAgent)
	if c.APIVersion != "" {
		req.Header.Set("X-GitHub-Api-Version", c.APIVersion)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("call GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited := io.LimitReader(resp.Body, 64<<10)
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(limited).Decode(&payload)
		return &APIError{StatusCode: resp.StatusCode, Message: payload.Message}
	}
	if result == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}

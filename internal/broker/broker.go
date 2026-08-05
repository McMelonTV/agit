package broker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/repository"
)

const (
	URLEnv    = "VIAGH_BROKER_URL"
	SecretEnv = "VIAGH_BROKER_SECRET"

	maxRequestBytes  = 64 << 10
	maxResponseBytes = 1 << 20
)

type Backend interface {
	ResolveToken(context.Context, repository.Ref, string) (githubapp.Token, githubapp.Installation, error)
	Installations(context.Context) ([]githubapp.Installation, error)
	Installation(context.Context, int64) (githubapp.Installation, error)
	TokenForInstallation(context.Context, int64) (githubapp.Token, error)
	BotIdentity(context.Context) (githubapp.Identity, error)
}

type Server struct {
	URL    string
	Secret string
	server *http.Server
	ln     net.Listener
}

type Client struct {
	URL        string
	Secret     string
	HTTPClient *http.Client
}

type tokenRequest struct {
	Repository repository.Ref `json:"repository"`
	Owner      string         `json:"owner,omitempty"`
}

type tokenResponse struct {
	Token        githubapp.Token        `json:"token"`
	Installation githubapp.Installation `json:"installation"`
}

type installationRequest struct {
	ID int64 `json:"id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func Start(backend Backend) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start credential broker: %w", err)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		listener.Close()
		return nil, fmt.Errorf("generate credential broker secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	mux := http.NewServeMux()
	s := &Server{
		URL:    "http://" + listener.Addr().String(),
		Secret: secret,
		ln:     listener,
	}
	mux.HandleFunc("/token", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request tokenRequest
		if err := decodeRequest(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		token, installation, err := backend.ResolveToken(r.Context(), request.Repository, request.Owner)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, tokenResponse{Token: token, Installation: installation})
	}))
	mux.HandleFunc("/installations", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		installations, err := backend.Installations(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, installations)
	}))
	mux.HandleFunc("/installation", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request installationRequest
		if err := decodeRequest(r, &request); err != nil || request.ID <= 0 {
			if err == nil {
				err = errors.New("installation ID must be positive")
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		installation, err := backend.Installation(r.Context(), request.ID)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, installation)
	}))
	mux.HandleFunc("/installation-token", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request installationRequest
		if err := decodeRequest(r, &request); err != nil || request.ID <= 0 {
			if err == nil {
				err = errors.New("installation ID must be positive")
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		token, err := backend.TokenForInstallation(r.Context(), request.ID)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, token)
	}))
	mux.HandleFunc("/bot-identity", s.authorize(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		identity, err := backend.BotIdentity(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, identity)
	}))

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		_ = s.server.Serve(listener)
	}()
	return s, nil
}

func FromEnv() (Client, bool) {
	rawURL := strings.TrimSpace(os.Getenv(URLEnv))
	secret := os.Getenv(SecretEnv)
	if rawURL == "" || secret == "" {
		return Client{}, false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return Client{}, false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return Client{}, false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return Client{}, false
	}
	return Client{URL: strings.TrimRight(rawURL, "/"), Secret: secret, HTTPClient: http.DefaultClient}, true
}

func (s *Server) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.Secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.Secret)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("invalid credential broker authorization"))
			return
		}
		next(w, r)
	}
}

func (c Client) ResolveToken(ctx context.Context, ref repository.Ref, owner string) (githubapp.Token, githubapp.Installation, error) {
	var response tokenResponse
	err := c.do(ctx, http.MethodPost, "/token", tokenRequest{Repository: ref, Owner: owner}, &response)
	return response.Token, response.Installation, err
}

func (c Client) Installations(ctx context.Context) ([]githubapp.Installation, error) {
	var result []githubapp.Installation
	err := c.do(ctx, http.MethodGet, "/installations", nil, &result)
	return result, err
}

func (c Client) Installation(ctx context.Context, id int64) (githubapp.Installation, error) {
	var result githubapp.Installation
	err := c.do(ctx, http.MethodPost, "/installation", installationRequest{ID: id}, &result)
	return result, err
}

func (c Client) TokenForInstallation(ctx context.Context, id int64) (githubapp.Token, error) {
	var result githubapp.Token
	err := c.do(ctx, http.MethodPost, "/installation-token", installationRequest{ID: id}, &result)
	return result, err
}

func (c Client) BotIdentity(ctx context.Context) (githubapp.Identity, error) {
	var result githubapp.Identity
	err := c.do(ctx, http.MethodGet, "/bot-identity", nil, &result)
	return result, err
}

func (c Client) do(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		contents, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(contents)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, reader)
	if err != nil {
		return fmt.Errorf("create credential broker request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call credential broker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload errorResponse
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload)
		if payload.Error == "" {
			payload.Error = resp.Status
		}
		return errors.New(payload.Error)
	}
	if result == nil {
		return nil
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read credential broker response: %w", err)
	}
	if len(contents) > maxResponseBytes {
		return errors.New("credential broker response is too large")
	}
	if err := json.Unmarshal(contents, result); err != nil {
		return fmt.Errorf("decode credential broker response: %w", err)
	}
	return nil
}

func decodeRequest(r *http.Request, result any) error {
	defer r.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if len(contents) > maxRequestBytes {
		return errors.New("request is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("decode request: trailing data: %w", err)
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

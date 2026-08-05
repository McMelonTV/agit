package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/McMelonTV/viagh/internal/broker"
	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/repository"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

type localBackend struct {
	cfg         config.Config
	once        sync.Once
	client      *githubapp.Client
	err         error
	identityMu  sync.Mutex
	identity    githubapp.Identity
	identitySet bool
}

func (b *localBackend) appClient() (*githubapp.Client, error) {
	b.once.Do(func() {
		if err := b.cfg.ValidateCredentials(); err != nil {
			b.err = err
			return
		}
		privateKeyPEM := b.cfg.PrivateKeyPEM
		if privateKeyPEM == "" && b.cfg.PrivateKeyBase64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(b.cfg.PrivateKeyBase64)
			if err != nil {
				b.err = fmt.Errorf("decode VIAGH_PRIVATE_KEY_BASE64: %w", err)
				return
			}
			privateKeyPEM = string(decoded)
		}
		key, err := githubapp.LoadPrivateKey(b.cfg.PrivateKeyPath, privateKeyPEM)
		if err != nil {
			b.err = err
			return
		}
		httpClient := &http.Client{Timeout: b.cfg.HTTPTimeout}
		b.client = githubapp.NewClient(b.cfg.AppID, key, b.cfg.APIURL, b.cfg.APIVersion, httpClient)
		b.client.UserAgent = "viagh/" + version
	})
	return b.client, b.err
}

func (b *localBackend) ResolveToken(ctx context.Context, hint repository.Ref, owner string) (githubapp.Token, githubapp.Installation, error) {
	client, err := b.appClient()
	if err != nil {
		return githubapp.Token{}, githubapp.Installation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HTTPTimeout)
	defer cancel()
	installation, err := resolveInstallation(ctx, client, b.cfg, hint, owner)
	if err != nil {
		return githubapp.Token{}, githubapp.Installation{}, err
	}
	cache := githubapp.TokenCache{Dir: b.cfg.CacheDir, Disabled: b.cfg.NoCache}
	token, err := client.CachedInstallationToken(ctx, installation.ID, cache, b.cfg.RefreshBefore)
	if err != nil {
		return githubapp.Token{}, githubapp.Installation{}, fmt.Errorf("create installation token for %d: %w", installation.ID, err)
	}
	return token, installation, nil
}

func (b *localBackend) Installations(ctx context.Context) ([]githubapp.Installation, error) {
	client, err := b.appClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HTTPTimeout)
	defer cancel()
	return client.Installations(ctx)
}

func (b *localBackend) Installation(ctx context.Context, id int64) (githubapp.Installation, error) {
	client, err := b.appClient()
	if err != nil {
		return githubapp.Installation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HTTPTimeout)
	defer cancel()
	return client.Installation(ctx, id)
}

func (b *localBackend) TokenForInstallation(ctx context.Context, id int64) (githubapp.Token, error) {
	client, err := b.appClient()
	if err != nil {
		return githubapp.Token{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HTTPTimeout)
	defer cancel()
	cache := githubapp.TokenCache{Dir: b.cfg.CacheDir, Disabled: b.cfg.NoCache}
	return client.CachedInstallationToken(ctx, id, cache, b.cfg.RefreshBefore)
}

func (b *localBackend) BotIdentity(ctx context.Context) (githubapp.Identity, error) {
	b.identityMu.Lock()
	defer b.identityMu.Unlock()
	if b.identitySet {
		return b.identity, nil
	}
	client, err := b.appClient()
	if err != nil {
		return githubapp.Identity{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.HTTPTimeout)
	defer cancel()
	identity, err := client.BotIdentity(ctx)
	if err != nil {
		return githubapp.Identity{}, err
	}
	b.identity = identity
	b.identitySet = true
	return b.identity, nil
}

type authSession struct {
	cfg     config.Config
	backend broker.Backend
	server  *broker.Server
	client  broker.Client
	encoded string
}

func beginAuthSession(cfg config.Config) (*authSession, error) {
	encoded, err := config.EncodeSession(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.SessionRestricted {
		client, ok := broker.FromEnv()
		if !ok {
			return nil, errors.New("active viagh credential broker is unavailable")
		}
		return &authSession{cfg: cfg, backend: client, client: client, encoded: encoded}, nil
	}
	backend := &localBackend{cfg: cfg}
	server, err := broker.Start(backend)
	if err != nil {
		return nil, err
	}
	client := broker.Client{URL: server.URL, Secret: server.Secret, HTTPClient: &http.Client{Timeout: cfg.HTTPTimeout}}
	return &authSession{cfg: cfg, backend: client, server: server, client: client, encoded: encoded}, nil
}

func (s *authSession) Environment(base []string) []string {
	env := append([]string(nil), base...)
	if s.cfg.SessionRestricted {
		// A nested viagh process may inherit an installation token injected by
		// its parent. Remove that exact token before dropping the tracking hash;
		// otherwise a later pass-through command for another host could leak it.
		env = wrapper.RemoveActiveAppAuth(env)
	} else {
		// This marker is meaningful only inside a broker-backed child session.
		// Never trust a same-named variable from the user's original environment.
		env = wrapper.ClearGitAuthTracking(env)
	}
	env = wrapper.SanitizeEnv(env)
	env = setOptionalEnv(env, "VIAGH_INSTALLATION_ID", func() string {
		if s.cfg.InstallationID > 0 {
			return strconv.FormatInt(s.cfg.InstallationID, 10)
		}
		return ""
	}())
	env = setOptionalEnv(env, "VIAGH_OWNER", s.cfg.Owner)
	env = setOptionalEnv(env, "VIAGH_REPOSITORY", s.cfg.Repository)
	env = setOptionalEnv(env, "GH_REPO", s.cfg.Repository)
	env = wrapper.SetEnv(env, "GH_HOST", s.cfg.Host)
	env = setOptionalEnv(env, "VIAGH_REAL_GH", s.cfg.RealGH)
	env = setOptionalEnv(env, "VIAGH_REAL_GIT", s.cfg.RealGit)
	env = wrapper.UnsetEnv(env, "VIAGH_PASSTHROUGH")
	env = wrapper.SetEnv(env, config.SessionConfigEnv, s.encoded)
	env = wrapper.SetEnv(env, broker.URLEnv, s.client.URL)
	env = wrapper.SetEnv(env, broker.SecretEnv, s.client.Secret)
	return env
}

func setOptionalEnv(env []string, key, value string) []string {
	if value == "" {
		return wrapper.UnsetEnv(env, key)
	}
	return wrapper.SetEnv(env, key, value)
}

func (s *authSession) Close() {
	if s != nil && s.server != nil {
		_ = s.server.Close()
	}
}

func resolveInstallation(ctx context.Context, client *githubapp.Client, cfg config.Config, hint repository.Ref, owner string) (githubapp.Installation, error) {
	if cfg.InstallationID > 0 {
		return githubapp.Installation{ID: cfg.InstallationID}, nil
	}
	if hint.Owner != "" && hint.Repo != "" {
		installation, err := client.RepositoryInstallation(ctx, hint.Owner, hint.Repo)
		if err != nil {
			return githubapp.Installation{}, fmt.Errorf("find installation for %s: %w", hint.String(), err)
		}
		return installation, nil
	}
	if owner != "" {
		installation, err := client.OwnerInstallation(ctx, owner)
		if err != nil {
			return githubapp.Installation{}, fmt.Errorf("find installation for owner %s: %w", owner, err)
		}
		return installation, nil
	}
	if cfg.Owner != "" {
		installation, err := client.OwnerInstallation(ctx, cfg.Owner)
		if err != nil {
			return githubapp.Installation{}, fmt.Errorf("find installation for owner %s: %w", cfg.Owner, err)
		}
		return installation, nil
	}

	installations, err := client.Installations(ctx)
	if err != nil {
		return githubapp.Installation{}, fmt.Errorf("list GitHub App installations: %w", err)
	}
	switch len(installations) {
	case 0:
		return githubapp.Installation{}, errors.New("the GitHub App has no installations")
	case 1:
		return installations[0], nil
	default:
		choices := make([]string, 0, len(installations))
		for _, installation := range installations {
			label := strconv.FormatInt(installation.ID, 10)
			if installation.Account.Login != "" {
				label += " (" + installation.Account.Login + ")"
			}
			choices = append(choices, label)
		}
		return githubapp.Installation{}, fmt.Errorf("multiple GitHub App installations found: %s; set VIAGH_INSTALLATION_ID, VIAGH_OWNER, or VIAGH_REPOSITORY", strings.Join(choices, ", "))
	}
}

func resolveWithBackend(ctx context.Context, backend broker.Backend, cfg config.Config, hint repository.Ref, owner string) (githubapp.Token, githubapp.Installation, error) {
	if cfg.InstallationID > 0 {
		installation, err := backend.Installation(ctx, cfg.InstallationID)
		if err != nil {
			return githubapp.Token{}, githubapp.Installation{}, fmt.Errorf("get installation %d: %w", cfg.InstallationID, err)
		}
		token, err := backend.TokenForInstallation(ctx, cfg.InstallationID)
		if err != nil {
			return githubapp.Token{}, githubapp.Installation{}, fmt.Errorf("create installation token for %d: %w", cfg.InstallationID, err)
		}
		return token, installation, nil
	}
	if hint.Owner != "" && hint.Repo != "" {
		return backend.ResolveToken(ctx, hint, "")
	}
	if owner != "" {
		return backend.ResolveToken(ctx, repository.Ref{}, owner)
	}
	if cfg.Owner != "" {
		return backend.ResolveToken(ctx, repository.Ref{}, cfg.Owner)
	}
	return backend.ResolveToken(ctx, repository.Ref{}, "")
}

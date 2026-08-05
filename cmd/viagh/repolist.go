package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/McMelonTV/viagh/internal/broker"
	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/ghcmd"
	"github.com/McMelonTV/viagh/internal/githubapp"
	"github.com/McMelonTV/viagh/internal/repository"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func runRepoListAcrossInstallations(cfg config.Config, backend broker.Backend, ghPath string, args []string, self string, baseEnv []string) int {
	if _, err := ghcmd.RepoListLimit(args); err != nil {
		return fail(err)
	}
	installations, err := repoListInstallations(context.Background(), backend, cfg)
	if err != nil {
		return fail(err)
	}
	if len(installations) == 0 {
		return fail(errors.New("the GitHub App has no installations"))
	}
	installations, err = normalizeInstallations(context.Background(), backend, cfg, installations)
	if err != nil {
		return fail(err)
	}
	sort.SliceStable(installations, func(left, right int) bool {
		return strings.ToLower(installations[left].Account.Login) < strings.ToLower(installations[right].Account.Login)
	})

	if len(installations) == 1 {
		return runRepoListSingle(cfg, backend, ghPath, args, self, baseEnv, installations[0])
	}
	if ghcmd.RepoListHasFormatter(args) {
		return fail(errors.New("ownerless gh repo list cannot apply --jq or --template across multiple installations; use --json and format the merged array, or specify an owner"))
	}
	if ghcmd.RepoListRawJSON(args) {
		return runRepoListJSON(cfg, backend, ghPath, args, self, baseEnv, installations)
	}
	return runRepoListPlain(cfg, backend, ghPath, args, self, baseEnv, installations)
}

func runRepoListSingle(cfg config.Config, backend broker.Backend, ghPath string, args []string, self string, baseEnv []string, installation githubapp.Installation) int {
	token, err := tokenForInstallation(context.Background(), backend, cfg, installation.ID)
	if err != nil {
		return fail(fmt.Errorf("create installation token for %s (%d): %w", installation.Account.Login, installation.ID, err))
	}
	env, err := repoListEnv(cfg, self, baseEnv, token.Token)
	if err != nil {
		return fail(err)
	}
	return wrapper.Run(ghPath, ghcmd.WithRepoListOwner(args, installation.Account.Login), env)
}

func runRepoListJSON(cfg config.Config, backend broker.Backend, ghPath string, args []string, self string, baseEnv []string, installations []githubapp.Installation) int {
	limit, err := ghcmd.RepoListLimit(args)
	if err != nil {
		return fail(err)
	}
	merged := make([]json.RawMessage, 0)
	var stderr bytes.Buffer
	for _, installation := range installations {
		token, err := tokenForInstallation(context.Background(), backend, cfg, installation.ID)
		if err != nil {
			return fail(fmt.Errorf("create installation token for %s (%d): %w", installation.Account.Login, installation.ID, err))
		}
		env, err := repoListEnv(cfg, self, baseEnv, token.Token)
		if err != nil {
			return fail(err)
		}
		callArgs := ghcmd.WithRepoListOwner(ghcmd.WithRepoListLimit(args, limit), installation.Account.Login)
		stdout, callStderr, code, err := wrapper.Capture(ghPath, callArgs, env)
		stderr.Write(callStderr)
		if err != nil {
			writeBufferedStderr(&stderr)
			return fail(err)
		}
		if code != 0 {
			writeBufferedStderr(&stderr)
			return code
		}
		var batch []json.RawMessage
		if err := json.Unmarshal(stdout, &batch); err != nil {
			writeBufferedStderr(&stderr)
			return fail(fmt.Errorf("decode gh repo list JSON for %s: %w", installation.Account.Login, err))
		}
		merged = append(merged, batch...)
		if len(merged) >= limit {
			break
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	writeBufferedStderr(&stderr)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(merged); err != nil {
		return fail(fmt.Errorf("write merged repository list: %w", err))
	}
	return 0
}

func runRepoListPlain(cfg config.Config, backend broker.Backend, ghPath string, args []string, self string, baseEnv []string, installations []githubapp.Installation) int {
	limit, err := ghcmd.RepoListLimit(args)
	if err != nil {
		return fail(err)
	}
	var lines []string
	var stderr bytes.Buffer
	for _, installation := range installations {
		token, err := tokenForInstallation(context.Background(), backend, cfg, installation.ID)
		if err != nil {
			return fail(fmt.Errorf("create installation token for %s (%d): %w", installation.Account.Login, installation.ID, err))
		}
		env, err := repoListEnv(cfg, self, baseEnv, token.Token)
		if err != nil {
			return fail(err)
		}
		callArgs := ghcmd.WithRepoListOwner(ghcmd.WithRepoListLimit(args, limit), installation.Account.Login)
		stdout, callStderr, code, err := wrapper.Capture(ghPath, callArgs, env)
		stderr.Write(callStderr)
		if err != nil {
			writeBufferedStderr(&stderr)
			return fail(err)
		}
		if code != 0 {
			writeBufferedStderr(&stderr)
			return code
		}
		lines = append(lines, splitOutputLines(stdout)...)
		if len(lines) >= limit {
			break
		}
	}
	if len(lines) > limit {
		lines = lines[:limit]
	}
	writeBufferedStderr(&stderr)
	if len(lines) > 0 {
		_, _ = fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
	}
	return 0
}

func repoListInstallations(ctx context.Context, backend broker.Backend, cfg config.Config) ([]githubapp.Installation, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout)
	defer cancel()
	if cfg.InstallationID > 0 {
		installation, err := backend.Installation(ctx, cfg.InstallationID)
		if err != nil {
			return nil, fmt.Errorf("get installation %d: %w", cfg.InstallationID, err)
		}
		return []githubapp.Installation{installation}, nil
	}
	if cfg.Repository != "" {
		ref, ok := repository.Parse(cfg.Repository)
		if !ok {
			return nil, errors.New("repository must be in OWNER/REPO form")
		}
		_, installation, err := backend.ResolveToken(ctx, ref, "")
		if err != nil {
			return nil, fmt.Errorf("find installation for %s: %w", ref.String(), err)
		}
		return []githubapp.Installation{installation}, nil
	}
	if cfg.Owner != "" {
		_, installation, err := backend.ResolveToken(ctx, repository.Ref{}, cfg.Owner)
		if err != nil {
			return nil, fmt.Errorf("find installation for owner %s: %w", cfg.Owner, err)
		}
		return []githubapp.Installation{installation}, nil
	}
	installations, err := backend.Installations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list GitHub App installations: %w", err)
	}
	return installations, nil
}

func normalizeInstallations(ctx context.Context, backend broker.Backend, cfg config.Config, installations []githubapp.Installation) ([]githubapp.Installation, error) {
	seen := make(map[string]bool)
	result := make([]githubapp.Installation, 0, len(installations))
	for _, installation := range installations {
		if installation.Account.Login == "" {
			requestCtx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout)
			resolved, err := backend.Installation(requestCtx, installation.ID)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("get installation %d account: %w", installation.ID, err)
			}
			installation = resolved
		}
		login := strings.TrimSpace(installation.Account.Login)
		if login == "" {
			return nil, fmt.Errorf("installation %d has no account login", installation.ID)
		}
		key := strings.ToLower(login)
		if seen[key] {
			continue
		}
		seen[key] = true
		installation.Account.Login = login
		result = append(result, installation)
	}
	return result, nil
}

func tokenForInstallation(ctx context.Context, backend broker.Backend, cfg config.Config, installationID int64) (githubapp.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout)
	defer cancel()
	return backend.TokenForInstallation(ctx, installationID)
}

func repoListEnv(cfg config.Config, self string, baseEnv []string, token string) ([]string, error) {
	env := wrapper.ApplyGHAuth(baseEnv, cfg.Host, cfg.CloudHost(), token)
	return wrapper.ApplyGitAuth(env, self, cfg.Host)
}

func splitOutputLines(output []byte) []string {
	text := strings.TrimRight(string(output), "\r\n")
	if text == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func writeBufferedStderr(buffer *bytes.Buffer) {
	if buffer.Len() > 0 {
		_, _ = os.Stderr.Write(buffer.Bytes())
	}
}

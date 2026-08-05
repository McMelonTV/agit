package main

import (
	"context"
	"errors"

	"fmt"
	"os"

	"strings"

	"github.com/McMelonTV/viagh/internal/broker"
	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/ghcmd"
	"github.com/McMelonTV/viagh/internal/repository"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func runTool(cfg config.Config, tool string, args []string) int {
	if tool == "gh" && !ghcmd.IsAuthStatusJSONHosts(args) && !ghcmd.NeedsAuthentication(args) {
		return runGHUnauthenticated(cfg.RealGH, args)
	}

	path, err := wrapper.FindRealBinary(tool, configuredBinary(cfg, tool))
	if err != nil {
		return fail(err)
	}
	self, err := os.Executable()
	if err != nil {
		return fail(fmt.Errorf("locate viagh executable: %w", err))
	}
	self = wrapper.CleanExecutablePath(self)
	if tool == "gh" && ghcmd.IsAuthStatusJSONHosts(args) {
		host := cfg.Host
		if requestedHost, ok := ghcmd.AuthStatusHostname(args); ok {
			if !repository.HostMatches(requestedHost, cfg.Host) {
				return runGHUnauthenticatedWithRepository(path, args, "")
			}
			host = requestedHost
		}
		return runGitHubAppAuthStatus(cfg, host)
	}

	realGit := ""
	if tool == "git" {
		realGit = path
		cfg.RealGit = path
	} else if candidate, findErr := wrapper.FindRealBinary("git", cfg.RealGit); findErr == nil {
		realGit = candidate
		cfg.RealGit = candidate
	}
	var hint repository.Ref
	var owner string
	if tool == "gh" {
		cfg.RealGH = path
		hasExplicitHost := false
		if requestedHost, ok := ghcmd.TargetHostname(args); ok {
			hasExplicitHost = true
			if !repository.HostMatches(requestedHost, cfg.Host) {
				return runGHUnauthenticatedWithRepository(path, args, "")
			}
		}
		if requestedHost, ok := repository.ExplicitHost(tool, args); ok {
			hasExplicitHost = true
			if !repository.HostMatches(requestedHost, cfg.Host) {
				return runGHUnauthenticatedWithRepository(path, args, "")
			}
		}
		if !hasExplicitHost && cfg.SessionRestricted {
			if requestedHost := strings.TrimSpace(os.Getenv("GH_HOST")); requestedHost != "" && !repository.HostMatches(requestedHost, cfg.Host) {
				return runGHUnauthenticatedWithRepository(path, args, "")
			}
		}

		argumentRepository, hasArgumentRepository := repository.FromRepoFlag(args, cfg.Host)
		owner, _ = ghcmd.Owner(args)
		positionalRepository, hasPositionalRepository := repository.FromArgs(tool, args, cfg.Host)
		_, hasUnqualifiedRepoCreate := ghcmd.RepoCreateUnqualifiedName(args)

		switch {
		case hasArgumentRepository:
			hint = argumentRepository
			owner = ""
		case owner != "":
			// Owner-scoped operations, including repository creation and a fork's
			// target organization, take precedence over ambient repository state.
			hint = repository.Ref{}
		case hasPositionalRepository:
			hint = positionalRepository
		case cfg.Repository != "":
			configuredRef, ok := repository.Parse(cfg.Repository)
			if ok && configuredRef.Host != "" && !repository.HostMatches(configuredRef.Host, cfg.Host) {
				return runGHUnauthenticatedWithRepository(path, args, cfg.Repository)
			}
			if ok {
				hint = configuredRef
			}
		case hasUnqualifiedRepoCreate:
			// An unqualified create target names a new repository, not the
			// repository in the current working directory. Resolve it from an
			// explicit owner/installation or the sole App installation, then
			// qualify the target after token selection below.
			hint = repository.Ref{}
		default:
			if ambient, ok := repository.CurrentAny(context.Background(), realGit, args); ok {
				if ambient.Host != "" && !repository.HostMatches(ambient.Host, cfg.Host) {
					repositoryOverride := ambient.Host + "/" + ambient.String()
					return runGHUnauthenticatedWithRepository(path, args, repositoryOverride)
				}
				hint = ambient
			}
		}
	}

	session, err := beginAuthSession(cfg)
	if err != nil {
		return fail(err)
	}
	defer session.Close()
	env := session.Environment(os.Environ())
	shimDir, cleanup, shimErr := wrapper.CreateShims(self)
	if shimErr != nil {
		return fail(shimErr)
	}
	defer cleanup()
	env = wrapper.PrependPath(env, shimDir)

	if tool == "git" {
		env, err = wrapper.ApplyGitAuth(env, self, cfg.Host)
		if err != nil {
			return fail(err)
		}
		args, env, identityCleanup, identityErr := applyGitAuthorship(cfg, session.backend, path, self, args, env)
		if identityErr != nil {
			return fail(identityErr)
		}
		defer identityCleanup()
		return wrapper.Run(path, args, env)
	}

	if ghcmd.IsRepoList(args) {
		if _, hasOwner := ghcmd.RepoListOwner(args); !hasOwner {
			return runRepoListAcrossInstallations(cfg, session.backend, path, args, self, env)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	token, installation, err := resolveWithBackend(ctx, session.backend, cfg, hint, owner)
	cancel()
	if err != nil {
		return fail(err)
	}
	if _, ok := ghcmd.RepoCreateUnqualifiedName(args); ok {
		account := strings.TrimSpace(installation.Account.Login)
		if account == "" {
			account = strings.TrimSpace(cfg.Owner)
		}
		if account == "" {
			return fail(errors.New("cannot determine the installation account for gh repo create; use OWNER/REPO or set VIAGH_OWNER"))
		}
		args = ghcmd.WithRepoCreateOwner(args, account)
	}
	env = wrapper.ApplyGHAuth(env, cfg.Host, cfg.CloudHost(), token.Token)
	env, err = wrapper.ApplyGitAuth(env, self, cfg.Host)
	if err != nil {
		return fail(err)
	}
	return wrapper.Run(path, args, env)
}

func runGHUnauthenticated(configured string, args []string) int {
	return runGHUnauthenticatedWithRepository(configured, args, "")
}

func runGHUnauthenticatedWithRepository(configured string, args []string, repositoryOverride string) int {
	path, err := wrapper.FindRealBinary("gh", configured)
	if err != nil {
		return fail(err)
	}
	env := passthroughEnvironment(os.Environ())
	if repositoryOverride != "" {
		env = wrapper.SetEnv(env, "GH_REPO", repositoryOverride)
		if ref, ok := repository.Parse(repositoryOverride); ok && ref.Host != "" {
			env = wrapper.SetEnv(env, "GH_HOST", ref.Host)
		}
	}
	return wrapper.Run(path, args, env)
}

func runPassthroughTool(tool string, args []string) int {
	configured := os.Getenv("VIAGH_REAL_GIT")
	if tool == "gh" {
		configured = os.Getenv("VIAGH_REAL_GH")
	}
	path, err := wrapper.FindRealBinary(tool, configured)
	if err != nil {
		return fail(err)
	}
	return wrapper.Run(path, args, passthroughEnvironment(os.Environ()))
}

func passthroughEnvironment(base []string) []string {
	env := wrapper.RemoveActiveAppAuth(base)
	env = wrapper.SanitizeEnv(env)
	if session, ok := wrapper.LookupEnv(env, config.SessionConfigEnv); ok && session != "" {
		env = wrapper.StripGitAuth(env)
	} else {
		env = wrapper.ClearGitAuthTracking(env)
	}
	env = wrapper.UnsetEnv(env, config.SessionConfigEnv)
	env = wrapper.UnsetEnv(env, broker.URLEnv)
	env = wrapper.UnsetEnv(env, broker.SecretEnv)
	env = wrapper.SetEnv(env, "VIAGH_PASSTHROUGH", "1")
	return env
}

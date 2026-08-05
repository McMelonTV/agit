package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/McMelonTV/viagh/internal/broker"
	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/opencode"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func runCommitMessageHook(args []string) int {
	if len(args) != 1 || args[0] == "" {
		return fail(errors.New("commit-message-hook requires a commit message file"))
	}
	path := args[0]
	info, err := os.Stat(path)
	if err != nil {
		return fail(fmt.Errorf("inspect commit message: %w", err))
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fail(fmt.Errorf("read commit message: %w", err))
	}
	env := os.Environ()
	updated := wrapper.AppendCoauthorTrailer(contents, wrapper.OpenCodeTrailerFromEnv(env))
	updated = wrapper.AppendCoauthorTrailer(updated, wrapper.CoauthorTrailerFromEnv(env))
	if string(updated) == string(contents) {
		return 0
	}
	if err := os.WriteFile(path, updated, info.Mode().Perm()); err != nil {
		return fail(fmt.Errorf("write commit message: %w", err))
	}
	return 0
}

func applyGitAuthorship(cfg config.Config, backend broker.Backend, gitPath, self string, args, env []string) ([]string, []string, func(), error) {
	cleanup := func() {}
	if !cfg.OverrideGitIdentity && !cfg.OpenCodeTrailer {
		return args, env, cleanup, nil
	}

	mayCreateCommit := wrapper.GitMayCreateCommit(args)
	commandIsAlias := false
	if !mayCreateCommit {
		commandIsAlias = wrapper.GitCommandIsAlias(gitPath, args, env)
		mayCreateCommit = commandIsAlias
	}
	if !mayCreateCommit {
		return args, env, cleanup, nil
	}

	opencodeTrailer := ""
	if cfg.OpenCodeTrailer {
		opencodeTrailer = discoverOpenCodeTrailer(gitPath, args, env)
	}
	if !cfg.OverrideGitIdentity && opencodeTrailer == "" {
		return args, env, cleanup, nil
	}

	coauthor := ""
	if cfg.OverrideGitIdentity {
		configured := wrapper.GitIdentity{Name: cfg.GitName, Email: cfg.GitEmail}
		mode := cfg.GitAuthorship
		selected := configured
		var bot wrapper.GitIdentity
		if mode == config.GitAuthorshipBot || mode == config.GitAuthorshipBoth {
			ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
			identity, err := backend.BotIdentity(ctx)
			cancel()
			if err != nil {
				return nil, nil, cleanup, fmt.Errorf("resolve GitHub App bot identity: %w", err)
			}
			bot = wrapper.GitIdentity{Name: identity.Name, Email: identity.Email}
			if !bot.Valid() {
				return nil, nil, cleanup, errors.New("GitHub returned an incomplete App bot identity")
			}
			if mode == config.GitAuthorshipBot {
				selected = bot
			}
		}
		if !selected.Valid() {
			// Config normalization normally performs this fallback. Keep it here for
			// callers that construct Config directly rather than using Parse.
			ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
			identity, err := backend.BotIdentity(ctx)
			cancel()
			if err != nil {
				return nil, nil, cleanup, fmt.Errorf("resolve fallback GitHub App bot identity: %w", err)
			}
			bot = wrapper.GitIdentity{Name: identity.Name, Email: identity.Email}
			selected = bot
			mode = config.GitAuthorshipBot
		}

		var err error
		env, err = wrapper.ApplyGitIdentity(env, selected)
		if err != nil {
			return nil, nil, cleanup, err
		}
		if mode == config.GitAuthorshipBoth && bot.Valid() {
			coauthor = bot.Trailer()
		}
	}

	if opencodeTrailer == "" && coauthor == "" {
		return args, env, cleanup, nil
	}

	env = wrapper.SetCoauthorTrailer(env, coauthor)
	env = wrapper.SetOpenCodeTrailer(env, opencodeTrailer)
	if opencodeTrailer != "" {
		args = wrapper.AddCommitTreeTrailer(args, opencodeTrailer)
	}
	if coauthor != "" {
		args = wrapper.AddCommitTreeTrailer(args, coauthor)
	}
	if !wrapper.GitUsesCommitMessageHook(args) && !commandIsAlias {
		return args, env, cleanup, nil
	}
	marker := opencodeTrailer
	if marker == "" {
		marker = coauthor
	}
	hooksPath, hookCleanup, err := wrapper.PrepareCommitHooks(gitPath, self, args, env, marker)
	if err != nil {
		return nil, nil, cleanup, err
	}
	cleanup = hookCleanup
	if hooksPath == "" {
		return args, env, cleanup, nil
	}
	env, err = wrapper.AppendGitConfig(env, "core.hooksPath", hooksPath)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	return args, env, cleanup, nil
}

func discoverOpenCodeTrailer(gitPath string, args, env []string) string {
	// git resolves symlinks in its top-level path (for example /var to
	// /private/var on macOS), while opencode session directories usually keep
	// the unresolved path. Try both so either representation matches.
	dirs := []string{}
	if top, ok := wrapper.GitTopLevel(gitPath, args, env); ok {
		dirs = append(dirs, top)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		dirs = append(dirs, cwd)
	}
	for _, dir := range dirs {
		if trailer := opencode.DiscoverTrailer(dir, opencode.DiscoverTimeout); trailer != "" {
			return trailer
		}
	}
	return ""
}

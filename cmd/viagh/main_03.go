package main

import (
	"context"
	"errors"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/viagh/internal/broker"
	"github.com/McMelonTV/viagh/internal/config"

	"github.com/McMelonTV/viagh/internal/repository"
	"github.com/McMelonTV/viagh/internal/wrapper"
)

func runExec(cfg config.Config, args []string) int {
	if len(args) == 0 {
		return fail(errors.New("exec requires a command after --"))
	}

	if path, err := wrapper.FindRealBinary("git", cfg.RealGit); err == nil {
		cfg.RealGit = path
	}
	if path, err := wrapper.FindRealBinary("gh", cfg.RealGH); err == nil {
		cfg.RealGH = path
	}
	self, err := os.Executable()
	if err != nil {
		return fail(fmt.Errorf("locate viagh executable: %w", err))
	}
	self = wrapper.CleanExecutablePath(self)

	session, err := beginAuthSession(cfg)
	if err != nil {
		return fail(err)
	}
	defer session.Close()
	shimDir, cleanup, err := wrapper.CreateShims(self)
	if err != nil {
		return fail(err)
	}
	defer cleanup()

	env := session.Environment(os.Environ())
	env = wrapper.PrependPath(env, shimDir)
	env, err = wrapper.ApplyGitAuth(env, self, cfg.Host)
	if err != nil {
		return fail(err)
	}

	command := args[0]
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
	if base == "gh" || base == "git" {
		name := base
		if os.PathSeparator == '\\' {
			name += ".exe"
		}
		command = filepath.Join(shimDir, name)
	} else if !strings.ContainsRune(command, os.PathSeparator) {
		resolved, lookErr := exec.LookPath(command)
		if lookErr != nil {
			return fail(lookErr)
		}
		command = resolved
	}
	return wrapper.Run(command, args[1:], env)
}

func runToken(cfg config.Config) int {
	backend, err := backendFor(cfg)
	if err != nil {
		return fail(err)
	}
	realGit, _ := wrapper.FindRealBinary("git", cfg.RealGit)
	hint, _ := discoverRepository(context.Background(), cfg, "token", nil, realGit)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	defer cancel()
	token, _, err := resolveWithBackend(ctx, backend, cfg, hint, "")
	if err != nil {
		return fail(err)
	}
	fmt.Println(token.Token)
	return 0
}

func runInstallations(cfg config.Config) int {
	backend, err := backendFor(cfg)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	defer cancel()
	installations, err := backend.Installations(ctx)
	if err != nil {
		return fail(err)
	}
	fmt.Println("ID\tACCOUNT\tTYPE")
	for _, installation := range installations {
		fmt.Printf("%d\t%s\t%s\n", installation.ID, installation.Account.Login, installation.Account.Type)
	}
	return 0
}

func runDoctor(cfg config.Config) int {
	fmt.Printf("host: %s\n", cfg.Host)
	fmt.Printf("api: %s\n", cfg.APIURL)
	fmt.Printf("git identity override: %t\n", cfg.OverrideGitIdentity)
	fmt.Printf("git authorship: %s\n", cfg.GitAuthorship)
	if cfg.GitName != "" || cfg.GitEmail != "" {
		fmt.Printf("configured Git identity: %s <%s>\n", cfg.GitName, cfg.GitEmail)
	}
	if path, err := wrapper.FindRealBinary("git", cfg.RealGit); err == nil {
		fmt.Printf("git: %s\n", path)
	} else {
		fmt.Printf("git: unavailable (%v)\n", err)
	}
	if path, err := wrapper.FindRealBinary("gh", cfg.RealGH); err == nil {
		fmt.Printf("gh: %s\n", path)
	} else {
		fmt.Printf("gh: unavailable (%v)\n", err)
	}

	backend, err := backendFor(cfg)
	if err != nil {
		return fail(err)
	}
	realGit, _ := wrapper.FindRealBinary("git", cfg.RealGit)
	hint, _ := discoverRepository(context.Background(), cfg, "doctor", nil, realGit)
	if hint.Owner != "" {
		fmt.Printf("repository: %s\n", hint.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	token, installation, err := resolveWithBackend(ctx, backend, cfg, hint, "")
	cancel()
	if err != nil {
		return fail(err)
	}
	account := installation.Account.Login
	if account == "" {
		account = "not fetched"
	}
	fmt.Printf("installation: %d (%s)\n", installation.ID, account)
	fmt.Printf("token: valid until %s\n", token.ExpiresAt.UTC().Format(time.RFC3339))
	return 0
}

func backendFor(cfg config.Config) (broker.Backend, error) {
	if cfg.SessionRestricted {
		if client, ok := broker.FromEnv(); ok {
			return client, nil
		}
		return nil, errors.New("active viagh credential broker is unavailable")
	}
	return &localBackend{cfg: cfg}, nil
}

func discoverRepository(ctx context.Context, cfg config.Config, tool string, args []string, realGit string) (repository.Ref, bool) {
	if ref, ok := discoverExplicitRepository(cfg, tool, args); ok {
		return ref, true
	}
	return repository.Current(ctx, realGit, args, cfg.Host)
}

func discoverExplicitRepository(cfg config.Config, tool string, args []string) (repository.Ref, bool) {
	if ref, ok := repository.FromArgs(tool, args, cfg.Host); ok {
		return ref, true
	}
	if cfg.Repository != "" {
		if ref, ok := repository.Parse(cfg.Repository); ok {
			return ref, true
		}
	}
	return repository.Ref{}, false
}

func configuredBinary(cfg config.Config, tool string) string {
	if tool == "gh" {
		return cfg.RealGH
	}
	return cfg.RealGit
}

func shimTool(path string) (string, bool) {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
	if base == "gh" || base == "git" {
		return base, true
	}
	return "", false
}

func rawTopLevelCommand(args []string) (int, bool) {
	valueFlags := map[string]bool{
		"--app-id": true, "--private-key": true, "--installation-id": true,
		"--owner": true, "--repository": true, "--host": true, "--api-url": true,
		"--api-version": true, "--cache-dir": true, "--refresh-before": true,
		"--http-timeout": true, "--real-gh": true, "--real-git": true,
		"--git-name": true, "--git-email": true, "--git-authorship": true,
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 < len(args) {
				return index + 1, true
			}
			return 0, false
		}
		if strings.HasPrefix(arg, "--") {
			name, _, hasValue := strings.Cut(arg, "=")
			if valueFlags[name] {
				if !hasValue {
					index++
				}
				continue
			}
			if name == "--no-cache" || name == "--override-git-identity" {
				continue
			}
		}
		return index, true
	}
	return 0, false
}

func rawGlobalFlagValue(args []string, name string) (string, bool) {
	var value string
	found := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if key, attached, ok := strings.Cut(arg, "="); ok && key == name {
			value, found = attached, true
			continue
		}
		if arg == name && index+1 < len(args) {
			value, found = args[index+1], true
			index++
		}
	}
	return value, found
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "viagh:", err)
	return 1
}

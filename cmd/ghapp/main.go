package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/agit/internal/broker"
	"github.com/McMelonTV/agit/internal/config"
	"github.com/McMelonTV/agit/internal/ghcmd"
	"github.com/McMelonTV/agit/internal/repository"
	"github.com/McMelonTV/agit/internal/wrapper"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if len(argv) == 1 {
		printUsage()
		return 2
	}
	if len(argv) == 2 {
		switch argv[1] {
		case "version", "--version":
			fmt.Println("ghapp " + version)
			return 0
		case "help", "--help", "-h":
			printUsage()
			return 0
		}
	}
	if len(argv) >= 2 && argv[1] == "commit-message-hook" {
		return runCommitMessageHook(argv[2:])
	}
	if tool, ok := shimTool(argv[0]); ok && os.Getenv("GHAPP_PASSTHROUGH") == "1" {
		return runPassthroughTool(tool, argv[1:])
	}

	// Git invokes credential helpers using the configured executable path. That
	// path can itself be a gh/git shim, especially on Windows where shims may be
	// copies rather than symlinks, so dispatch this internal command first.
	if len(argv) >= 2 && argv[1] == "credential-helper" {
		cfg, err := config.FromEnv()
		if err != nil {
			if len(argv) >= 3 && argv[2] == "get" {
				return credentialFailure(err)
			}
			return fail(err)
		}
		return runCredentialHelper(cfg, argv[2:])
	}

	if tool, ok := shimTool(argv[0]); ok {
		if tool == "gh" && !ghcmd.NeedsAuthentication(argv[1:]) {
			return runGHUnauthenticated(os.Getenv("GHAPP_REAL_GH"), argv[1:])
		}
		cfg, err := config.FromEnv()
		if err != nil {
			return fail(err)
		}
		return runTool(cfg, tool, argv[1:])
	}

	if commandIndex, ok := rawTopLevelCommand(argv[1:]); ok && argv[1+commandIndex] == "gh" {
		ghArgs := argv[2+commandIndex:]
		if !ghcmd.NeedsAuthentication(ghArgs) {
			configured := os.Getenv("GHAPP_REAL_GH")
			if value, found := rawGlobalFlagValue(argv[1:1+commandIndex], "--real-gh"); found {
				configured = value
			}
			return runGHUnauthenticated(configured, ghArgs)
		}
	}

	cfg, args, err := config.Parse(argv[1:])
	if errors.Is(err, flag.ErrHelp) {
		printUsage()
		return 0
	}
	if err != nil {
		return fail(err)
	}
	if len(args) == 0 {
		printUsage()
		return 2
	}

	command, commandArgs := args[0], args[1:]
	switch command {
	case "help", "--help", "-h":
		printUsage()
		return 0
	case "version", "--version":
		fmt.Println("ghapp " + version)
		return 0
	case "gh", "git":
		return runTool(cfg, command, commandArgs)
	case "exec":
		if len(commandArgs) > 0 && commandArgs[0] == "--" {
			commandArgs = commandArgs[1:]
		}
		return runExec(cfg, commandArgs)
	case "token":
		return runToken(cfg)
	case "installations":
		return runInstallations(cfg)
	case "doctor":
		return runDoctor(cfg)
	case "credential-helper":
		return runCredentialHelper(cfg, commandArgs)
	default:
		return fail(fmt.Errorf("unknown command %q", command))
	}
}

func runTool(cfg config.Config, tool string, args []string) int {
	if tool == "gh" && !ghcmd.NeedsAuthentication(args) {
		return runGHUnauthenticated(cfg.RealGH, args)
	}

	path, err := wrapper.FindRealBinary(tool, configuredBinary(cfg, tool))
	if err != nil {
		return fail(err)
	}
	self, err := os.Executable()
	if err != nil {
		return fail(fmt.Errorf("locate ghapp executable: %w", err))
	}
	self = wrapper.CleanExecutablePath(self)

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
			return fail(errors.New("cannot determine the installation account for gh repo create; use OWNER/REPO or set GHAPP_OWNER"))
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
	configured := os.Getenv("GHAPP_REAL_GIT")
	if tool == "gh" {
		configured = os.Getenv("GHAPP_REAL_GH")
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
	env = wrapper.SetEnv(env, "GHAPP_PASSTHROUGH", "1")
	return env
}

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
		return fail(fmt.Errorf("locate ghapp executable: %w", err))
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
		return nil, errors.New("active ghapp credential broker is unavailable")
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
	fmt.Fprintln(os.Stderr, "ghapp:", err)
	return 1
}

func printUsage() {
	fmt.Print(`ghapp transparently supplies GitHub App installation credentials to gh and git.

Usage:
  ghapp [global flags] gh [args...]
  ghapp [global flags] git [args...]
  ghapp [global flags] exec -- command [args...]
  ghapp [global flags] token
  ghapp [global flags] installations
  ghapp [global flags] doctor

Transparent shim mode:
  Invoke the ghapp binary through a symlink named "gh" or "git". The wrapper
  locates and executes the next real binary on PATH. GHAPP_REAL_GH and
  GHAPP_REAL_GIT can explicitly select the underlying executables.

Required credentials:
  GHAPP_APP_ID             GitHub App ID or client ID
  GHAPP_PRIVATE_KEY        Path to the App private key PEM

Installation selection, in order:
  GHAPP_INSTALLATION_ID, repository inferred from arguments/current remote,
  owner inferred from gh arguments, GHAPP_OWNER, or the only installation.

Multi-installation behavior:
  An ownerless "gh repo list" runs across App installations. Raw --json arrays
  are merged and --limit applies to the combined result.

Useful variables:
  GHAPP_REPOSITORY         OWNER/REPO
  GHAPP_HOST               GitHub hostname (default: github.com)
  GHAPP_API_URL            REST API base URL
  GHAPP_PRIVATE_KEY_PEM    Inline private key PEM
  GHAPP_PRIVATE_KEY_BASE64 Base64-encoded private key PEM
  GHAPP_NO_CACHE           Disable the on-disk token cache
  GHAPP_GIT_AUTHORSHIP     bot (default), configured, or both
  GHAPP_GIT_NAME           Name for configured or both authorship
  GHAPP_GIT_EMAIL          Email for configured or both authorship
  GHAPP_OVERRIDE_GIT_IDENTITY
                           Set false to preserve existing Git identity config
`)
}

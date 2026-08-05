package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/McMelonTV/viagh/internal/config"
	"github.com/McMelonTV/viagh/internal/ghcmd"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	if err := config.LoadEnvFile(); err != nil {
		return fail(err)
	}
	if len(argv) == 1 {
		printUsage()
		return 2
	}
	if len(argv) == 2 {
		switch argv[1] {
		case "version", "--version":
			fmt.Println("viagh " + version)
			return 0
		case "help", "--help", "-h":
			printUsage()
			return 0
		}
	}
	if len(argv) >= 2 && argv[1] == "commit-message-hook" {
		return runCommitMessageHook(argv[2:])
	}
	if tool, ok := shimTool(argv[0]); ok && os.Getenv("VIAGH_PASSTHROUGH") == "1" {
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
		if tool == "gh" && !ghcmd.IsAuthStatusJSONHosts(argv[1:]) && !ghcmd.NeedsAuthentication(argv[1:]) {
			return runGHUnauthenticated(os.Getenv("VIAGH_REAL_GH"), argv[1:])
		}
		cfg, err := config.FromEnv()
		if err != nil {
			return fail(err)
		}
		if err := validateRepoCreateOwner(cfg, tool, argv[1:]); err != nil {
			return fail(err)
		}
		return runTool(cfg, tool, argv[1:])
	}

	if commandIndex, ok := rawTopLevelCommand(argv[1:]); ok && argv[1+commandIndex] == "gh" {
		ghArgs := argv[2+commandIndex:]
		if !ghcmd.IsAuthStatusJSONHosts(ghArgs) && !ghcmd.NeedsAuthentication(ghArgs) {
			configured := os.Getenv("VIAGH_REAL_GH")
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
	if err := validateRepoCreateOwner(cfg, command, commandArgs); err != nil {
		return fail(err)
	}
	switch command {
	case "help", "--help", "-h":
		printUsage()
		return 0
	case "version", "--version":
		fmt.Println("viagh " + version)
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
	case "env":
		return runEnv(cfg, commandArgs)
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

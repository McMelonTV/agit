package main

import (
	"errors"
	"strings"

	"github.com/McMelonTV/agit/internal/config"
	"github.com/McMelonTV/agit/internal/ghcmd"
)

func validateRepoCreateOwner(cfg config.Config, tool string, args []string) error {
	if tool != "gh" {
		return nil
	}
	if _, ok := ghcmd.RepoCreateUnqualifiedName(args); !ok {
		return nil
	}
	if strings.TrimSpace(cfg.Owner) == "" {
		return errors.New("unqualified gh repo create target requires an explicit owner; use OWNER/REPO or set GHAPP_OWNER/--owner")
	}
	return nil
}

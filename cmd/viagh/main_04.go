package main

import (
	"fmt"
)

func printUsage() {
	fmt.Print(`viagh transparently supplies GitHub App installation credentials to gh and git.

Usage:
  viagh [global flags] gh [args...]
  viagh [global flags] git [args...]
  viagh [global flags] exec -- command [args...]
  viagh [global flags] token
  viagh [global flags] env KEY
  viagh [global flags] installations
  viagh [global flags] doctor

Transparent shim mode:
  Invoke the viagh binary through a symlink named "gh" or "git". The wrapper
  locates and executes the next real binary on PATH. VIAGH_REAL_GH and
  VIAGH_REAL_GIT can explicitly select the underlying executables.

Required credentials:
  VIAGH_APP_ID             GitHub App ID or client ID
  VIAGH_PRIVATE_KEY        Path to the App private key PEM

Installation selection, in order:
  VIAGH_INSTALLATION_ID, repository inferred from arguments/current remote,
  owner inferred from gh arguments, VIAGH_OWNER, or the only installation.

Multi-installation behavior:
  An ownerless "gh repo list" runs across App installations. Raw --json arrays
  are merged and --limit applies to the combined result.

Useful variables:
  VIAGH_REPOSITORY         OWNER/REPO
  VIAGH_HOST               GitHub hostname (default: github.com)
  VIAGH_API_URL            REST API base URL
  VIAGH_PRIVATE_KEY_PEM    Inline private key PEM
  VIAGH_PRIVATE_KEY_BASE64 Base64-encoded private key PEM
  VIAGH_NO_CACHE           Disable the on-disk token cache
  VIAGH_GIT_AUTHORSHIP     bot (default), configured, or both
  VIAGH_GIT_NAME           Name for configured or both authorship
  VIAGH_GIT_EMAIL          Email for configured or both authorship
  VIAGH_OVERRIDE_GIT_IDENTITY
                           Set false to preserve existing Git identity config
  VIAGH_OPENCODE_TRAILER   Append an opencode model attribution trailer when
                           running as an opencode subprocess
`)
}

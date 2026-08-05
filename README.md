# viagh

`viagh` wraps GitHub CLI (`gh`) and Git, authenticating commands with short-lived GitHub App installation tokens while keeping familiar `gh` and `git` command lines. It targets CI workers, service accounts, build hosts, and local automation that should operate as a GitHub App rather than as a human user.

A GitHub App token is not a user token. The App's installation scope and permissions still determine which repositories and APIs are available, and activity is attributed to the App.

## Key behavior

- Repository- and owner-scoped `gh` commands automatically select the matching App installation; a bare `gh repo list` fans out across all installations.
- Git authentication is lazy and restricted to the configured HTTPS host. Supported GitHub SSH and insecure HTTP remotes are rewritten to HTTPS only in the wrapped process.
- Nested commands can switch installations through a process-scoped credential broker; the App private key never reaches `gh`, Git hooks, extensions, or commands launched through `viagh exec`.
- Commands for a different GitHub host retain their existing user authentication and credential helpers.
- Commit authorship defaults to the App bot and can instead use a configured identity or add the bot as a co-author.

## Requirements

- Git, and GitHub CLI for `gh` wrapping.
- A GitHub App installed on the accounts or organizations to access, its private key, and repository **Contents** permission for HTTPS Git operations.
- Building from source requires Go 1.25 or newer.

## Build

```sh
go build -trimpath -o viagh ./cmd/viagh
# or: make check && make build
```

## Configure

At minimum, provide the App ID and private key (`GITHUB_APP_ID` also works for the ID):

```sh
export VIAGH_APP_ID=123456
export VIAGH_PRIVATE_KEY=/secure/path/app.private-key.pem
```

Alternatives: `VIAGH_PRIVATE_KEY_PEM` for an inline PEM, `VIAGH_PRIVATE_KEY_BASE64` for a base64-encoded PEM, or `GITHUB_APP_PRIVATE_KEY`. Installation selection is usually inferred from the command or current repository; constrain it explicitly with `VIAGH_INSTALLATION_ID`, `VIAGH_OWNER`, or `VIAGH_REPOSITORY`.

For GitHub Enterprise Server set `VIAGH_HOST` and, when needed, `VIAGH_API_URL` (absolute HTTPS URL; plain HTTP is allowed only for loopback addresses).

### Environment file

Variables are also loaded from `$XDG_CONFIG_HOME/viagh/viagh.env` (Linux/macOS, usually `~/.config/viagh/viagh.env`) or `%AppData%\viagh\viagh.env` (Windows) at every invocation. Set `VIAGH_CONFIG_FILE` to load another path; a template is at `.config/viagh/viagh.env`.

File rules: `KEY=VALUE` lines (optionally `export`-prefixed), blank lines and `#` comments skipped, existing environment wins, empty values ignored, values are literal (no shell expansion — quote values containing spaces), and a malformed file aborts the invocation.

### Git authorship

Commit-producing Git commands override `user.name`/`user.email` by default, with these modes:

| Mode | Result |
| --- | --- |
| `bot` (default) | The App bot is used for authorship and committer identity. |
| `configured` | `VIAGH_GIT_NAME` and `VIAGH_GIT_EMAIL` are used. |
| `both` | The configured identity is used and the App bot is added as a `Co-authored-by` trailer. |

If either configured identity field is empty, `configured` and `both` fall back to `bot`. Preserve the client identity with `VIAGH_OVERRIDE_GIT_IDENTITY=false`. Existing Git hooks run before the idempotent bot co-author trailer is appended, commit-producing aliases get the same controls, and history-preserving operations (cherry-pick, rebase, apply) keep the original author but still use the selected viagh committer identity. External `git-*` executables are outside command classification; run them through `viagh exec` or configure their identity explicitly.

### OpenCode attribution trailer

`VIAGH_OPENCODE_TRAILER=1` (or `--opencode-trailer`) appends a trailer identifying the opencode model and version to commits made while running as an opencode subprocess, e.g. ``Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`.`` Discovery queries the local opencode server, is best-effort (unreachable server or missing session silently skips the trailer), and is idempotent on re-runs.

## Usage

Explicit wrapper mode:

```sh
viagh gh pr list --repo acme/widgets
viagh gh api /orgs/acme/repos
viagh git clone git@github.com:acme/widgets.git
viagh git push origin main
viagh exec -- make release
viagh token          # print an installation token
viagh env VIAGH_APP_ID
viagh installations
viagh doctor
viagh version
```

`viagh exec` runs an arbitrary command with process-local `gh` and `git` shims, so nested calls can use different installations:

```sh
viagh exec -- sh -c 'gh repo view acme/one && gh repo view beta/two'
```

`viagh token` prints a token and should be used only where exposing a token through an environment variable is acceptable. `viagh env KEY` prints a variable's value and source (`env` or `env_file`), exiting nonzero when unset.

## Transparent shim mode

Invoke the same executable through links named `gh` and `git` to use App authentication without changing syntax:

```sh
export VIAGH_REAL_GH="$(command -v gh)"
export VIAGH_REAL_GIT="$(command -v git)"
install -m 0755 ./viagh "$HOME/.local/bin/viagh"
mkdir -p "$HOME/.local/libexec/viagh-shims"
ln -sfn "$HOME/.local/bin/viagh" "$HOME/.local/libexec/viagh-shims/gh"
ln -sfn "$HOME/.local/bin/viagh" "$HOME/.local/libexec/viagh-shims/git"
export PATH="$HOME/.local/libexec/viagh-shims:$PATH"
```

`VIAGH_REAL_GH` and `VIAGH_REAL_GIT` are recommended for deterministic CI and service environments. Without them, the wrapper finds the next executable on `PATH` while skipping itself.

### `gh auth status` and T3 Code

`gh auth status` is answered by viagh itself: the plain report mirrors the GitHub CLI text format (hostname, logged-in App bot account, active account, Git protocol), and the machine-readable `--json hosts` probe returns the same shape GitHub CLI produces, which is how T3 Code discovers authentication. `--hostname` and `--active` are honored; a request for another host is passed through to the real GitHub CLI. Neither path selects an installation or mints a token. Token display (`--show-token`), `--jq`, `--template`, and interactive auth management continue to use the underlying GitHub CLI credential store.

## Installation selection

For authenticated `gh` operations, selection follows this order:

1. `VIAGH_INSTALLATION_ID` or `--installation-id`.
2. An explicit `--repo`/`-R` repository.
3. An owner inferred from commands such as `gh repo list OWNER`, `gh project list --owner`, `gh secret list --org`, repository creation, or an owner-bearing REST path.
4. A positional repository accepted by the relevant `gh repo` command.
5. `VIAGH_REPOSITORY`, `GH_REPO`, or `--repository`.
6. The current repository, using GitHub CLI-compatible remote preferences.
7. `VIAGH_OWNER` or `--owner`.
8. The App's only installation.

For Git, the credential helper uses the exact HTTPS host and repository path supplied by Git. If no repository can be determined, an explicit owner or installation ID is required. If multiple installations remain and no target can be inferred, `viagh` returns an error rather than choosing one arbitrarily.

## Multi-installation `gh repo list`

An ownerless `viagh gh repo list --limit 100` fans out across all installations: processed deterministically by account name, `--limit` applies to the combined result, plain output is buffered until every invocation succeeds, and unformatted `--json` arrays are merged into one valid array (`[]` with no results). `--jq` and `--template` are rejected because per-installation formatting would be misleading; specify an owner or format the merged JSON afterward. `VIAGH_INSTALLATION_ID`, `VIAGH_OWNER`, or `VIAGH_REPOSITORY` restricts the fan-out.

## Git authentication design

Wrapped Git commands receive process-local configuration that registers `viagh credential-helper` only for `https://<VIAGH_HOST>`, enables `credential.useHttpPath` for repository-specific installation selection, clears conflicting authorization headers only for that host, and rewrites supported remote schemes to HTTPS without changing repository configuration. The helper returns no credential for other hosts or protocols, leaving unrelated credentials untouched.

A loopback broker owned by the top-level `viagh` process holds the App credentials and mints installation tokens on demand. Nested wrappers receive only a random, process-scoped broker capability and a configuration record with private-key fields removed; the broker shuts down when the wrapped command exits.

Authentication is skipped for local or informational `gh` operations (help, version, completion, configuration, alias management, auth inspection), and any command targeting a host other than `VIAGH_HOST` is passed to the real `gh` without the App token or broker environment.

## Configuration reference

| Variable | Purpose |
| --- | --- |
| `VIAGH_CONFIG_FILE` | Environment file to load at startup; defaults to `$XDG_CONFIG_HOME/viagh/viagh.env`. |
| `VIAGH_APP_ID` | App ID or client ID used as the JWT issuer. |
| `VIAGH_PRIVATE_KEY` / `VIAGH_PRIVATE_KEY_PEM` / `VIAGH_PRIVATE_KEY_BASE64` | Private-key path, inline PEM, or base64-encoded PEM. |
| `VIAGH_INSTALLATION_ID` | Explicit installation ID. |
| `VIAGH_OWNER` | Organization or user installation owner. |
| `VIAGH_REPOSITORY` | Repository selector in `OWNER/REPO` or `HOST/OWNER/REPO` form. |
| `VIAGH_HOST` | GitHub hostname; defaults to `github.com`. |
| `VIAGH_API_URL` / `VIAGH_API_VERSION` | REST API base URL and version (default `2026-03-10`). |
| `VIAGH_CACHE_DIR` / `VIAGH_NO_CACHE` | Installation-token cache directory / disable caching. |
| `VIAGH_REFRESH_BEFORE` / `VIAGH_HTTP_TIMEOUT` | Cache refresh threshold (`5m`) and API/broker timeout (`30s`). |
| `VIAGH_REAL_GH` / `VIAGH_REAL_GIT` | Underlying executables for shim mode. |
| `VIAGH_GIT_NAME` / `VIAGH_GIT_EMAIL` | Identity for `configured` or `both` authorship. |
| `VIAGH_GIT_AUTHORSHIP` | `bot`, `configured`, or `both`; defaults to `bot`. |
| `VIAGH_OVERRIDE_GIT_IDENTITY` | Override existing Git identity; defaults to true. |
| `VIAGH_OPENCODE_TRAILER` | Append the opencode attribution trailer to commits; defaults to false. |

Equivalent global flags are accepted before the `viagh` subcommand; run `viagh help` for the concise reference.

## Token cache

Installation tokens are cached in the user cache directory by default: real directories only (no symlinks), restricted to the current user where the platform permits, files mode `0600` on Unix-like systems, with an inter-process lock against parallel minting. Corrupt, unreadable, or expired entries are treated as misses; if a safe cache directory cannot be determined, caching is disabled rather than falling back to a shared temporary directory.

## Security considerations

- Tokens never appear in command-line arguments or repository URLs; the App private key is retained by the top-level broker and stripped from child environments.
- Installation tokens are visible to the wrapped child process. Descendants of `viagh exec`, Git hooks, and extensions inherit the broker capability, so treat arbitrary wrapped commands as trusted for the lifetime of that invocation.
- Cached tokens remain sensitive until expiry. The wrapper cannot exceed App permissions, repository selection, organization policy, or endpoint support.
- Use process isolation, a secret manager, least-privilege App permissions, and private-key rotation appropriate to the deployment.

## Development and releases

```sh
make check
VERSION=0.3.0 make release
```

The implementation uses only the Go standard library. CI runs tests on Linux, macOS, and Windows with the race detector and `go vet`, and cross-compiles amd64/arm64 for all three platforms. Pushing a `v*` tag creates a GitHub release with `.tar.gz`/`.zip` archives and SHA-256 checksums.

## References

- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app>
- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation>
- <https://cli.github.com/manual/gh_help_environment>
- <https://git-scm.com/docs/gitcredentials>

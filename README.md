# ghapp

`ghapp` is a Go wrapper around GitHub CLI (`gh`) and Git. It authenticates commands with short-lived GitHub App installation tokens while retaining familiar `gh` and `git` command lines.

It is intended for CI workers, service accounts, build hosts, and local automation that should operate as a GitHub App rather than as a human user.

## Key behavior

- Repository- and owner-scoped `gh` commands automatically select the matching App installation.
- A bare `gh repo list` fans out across all App installations.
- Git authentication is lazy and restricted to the configured GitHub HTTPS host.
- GitHub SSH, SSH-over-port-443, `git://`, and insecure HTTP remotes are rewritten to HTTPS only in the wrapped process.
- Nested commands can switch installations through a process-scoped credential broker.
- App private-key material is not inherited by `gh`, Git hooks, extensions, or commands launched through `ghapp exec`.
- Commands for a different GitHub host retain their existing user authentication and credential helpers.

A GitHub App token is not a user token. The App's installation scope and permissions still determine which repositories and APIs are available, and activity is attributed to the App.

## Requirements

- Git.
- GitHub CLI for `gh` wrapping.
- A GitHub App installed on the accounts or organizations to access.
- The App's private key.
- Repository **Contents** permission for HTTPS Git operations.

The module retains Go 1.23 language compatibility. CI and release binaries use currently supported Go toolchains, including Go 1.26.5.

## Build

```sh
go build -trimpath -o ghapp ./cmd/ghapp
```

Or:

```sh
make check
make build
```

## Configure

At minimum, provide the App ID and private key:

```sh
export GHAPP_APP_ID=123456
export GHAPP_PRIVATE_KEY=/secure/path/app.private-key.pem
```

The App ID may also be supplied as `GITHUB_APP_ID`. Private-key alternatives are:

```sh
export GHAPP_PRIVATE_KEY_PEM="$(cat /secure/path/app.private-key.pem)"
export GHAPP_PRIVATE_KEY_BASE64="$(base64 < /secure/path/app.private-key.pem | tr -d '\n')"
```

Selection is usually inferred from the command or current repository. It can be constrained explicitly:

```sh
export GHAPP_INSTALLATION_ID=9876543
# or
export GHAPP_OWNER=acme
# or
export GHAPP_REPOSITORY=acme/widgets
```

For GitHub Enterprise Server:

```sh
export GHAPP_HOST=github.example.com
export GHAPP_API_URL=https://github.example.com/api/v3
```

`GHAPP_API_URL` must be an absolute HTTPS URL. Plain HTTP is accepted only for loopback addresses, which is useful for tests.

## Explicit wrapper mode

```sh
ghapp gh pr list --repo acme/widgets
ghapp gh project list --owner acme
ghapp gh secret list --org acme
ghapp gh api /orgs/acme/repos

ghapp git clone git@github.com:acme/widgets.git
ghapp git fetch --all
ghapp git push origin main
```

Run an arbitrary command with process-local `gh` and `git` shims:

```sh
ghapp exec -- make release
```

Nested calls can use different installations:

```sh
ghapp exec -- sh -c '
  gh repo view acme/one
  gh repo view beta/two
  git clone git@github.com:acme/three.git
'
```

Other utility commands:

```sh
ghapp token
ghapp installations
ghapp doctor
ghapp version
```

`ghapp token` prints a token and should be used only where exposing a token through an environment variable is acceptable.

## Transparent shim mode

The same executable can be invoked through links named `gh` and `git`.

```sh
export GHAPP_REAL_GH="$(command -v gh)"
export GHAPP_REAL_GIT="$(command -v git)"

install -m 0755 ./ghapp "$HOME/.local/bin/ghapp"
mkdir -p "$HOME/.local/libexec/ghapp-shims"
ln -sfn "$HOME/.local/bin/ghapp" "$HOME/.local/libexec/ghapp-shims/gh"
ln -sfn "$HOME/.local/bin/ghapp" "$HOME/.local/libexec/ghapp-shims/git"
export PATH="$HOME/.local/libexec/ghapp-shims:$PATH"
```

Commands then use App authentication without changing their syntax:

```sh
gh pr list --repo acme/widgets
gh repo list acme
git fetch origin
git push origin main
```

`GHAPP_REAL_GH` and `GHAPP_REAL_GIT` are recommended for deterministic CI and service environments. Without them, the wrapper finds the next executable on `PATH` while skipping itself.

## Installation selection

For authenticated `gh` operations, selection follows this order:

1. `GHAPP_INSTALLATION_ID` or `--installation-id`.
2. An explicit `--repo`/`-R` repository.
3. An owner inferred from commands such as `gh repo list OWNER`, `gh project list --owner`, `gh secret list --org`, repository creation, or an owner-bearing REST path.
4. A positional repository accepted by the relevant `gh repo` command.
5. `GHAPP_REPOSITORY`, `GH_REPO`, or `--repository`.
6. The current repository selected using GitHub CLI-compatible remote preferences.
7. `GHAPP_OWNER` or `--owner`.
8. The App's only installation.

For Git, the credential helper uses the exact HTTPS host and repository path supplied by Git. If no repository can be determined, an explicit owner or installation ID is required.

If multiple installations remain and no target can be inferred, `ghapp` returns an error rather than choosing one arbitrarily.

## Multi-installation `gh repo list`

An ownerless repository list deliberately fans out across all installations:

```sh
ghapp gh repo list --limit 100
ghapp gh repo list --limit 100 --json nameWithOwner,isPrivate
```

Behavior across multiple installations:

- Installations are processed deterministically by account name.
- `--limit` is a global result limit, not a per-installation limit.
- Plain output is buffered and emitted only after every required invocation succeeds.
- Unformatted `--json` arrays are merged into one valid JSON array; no results produces `[]`.
- `--jq` and `--template` are rejected because applying them separately would produce misleading aggregate semantics. Specify an owner, or request merged JSON and format it afterward.
- `GHAPP_INSTALLATION_ID`, `GHAPP_OWNER`, or `GHAPP_REPOSITORY` restricts fan-out to the selected installation.

The combined plain result is grouped in installation account order; GitHub does not provide a cross-installation server-side ordering operation.

## Git authentication design

Wrapped Git commands receive process-local Git configuration that:

- registers `ghapp credential-helper` only for `https://<GHAPP_HOST>`;
- enables `credential.useHttpPath` for repository-specific installation selection;
- clears conflicting authorization headers only for that host;
- rewrites supported GitHub remote schemes to HTTPS without changing repository configuration.

The helper returns no credential for another host or protocol, leaving unrelated GitLab, Bitbucket, Enterprise, and private-host credentials untouched.

A loopback broker owned by the top-level `ghapp` process holds the App credentials and mints installation tokens on demand. Nested wrappers receive only a random, process-scoped broker capability and a configuration record with private-key fields removed. The broker shuts down when the wrapped command exits.

## Pass-through behavior

Authentication is skipped for local or informational `gh` operations such as help, version, completion, configuration, alias management, and authentication inspection.

A command targeting a host other than `GHAPP_HOST` is passed to the real `gh` without the App token or broker environment. This applies to explicit host/repository arguments, `GH_HOST`, and a current checkout whose selected remote belongs to another host.

## Configuration reference

| Variable | Purpose |
| --- | --- |
| `GHAPP_APP_ID` | App ID or client ID used as the JWT issuer. |
| `GHAPP_PRIVATE_KEY` | Path to the private-key PEM. |
| `GHAPP_PRIVATE_KEY_PEM` | Inline private-key PEM. |
| `GHAPP_PRIVATE_KEY_BASE64` | Base64-encoded private-key PEM. |
| `GHAPP_INSTALLATION_ID` | Explicit installation ID. |
| `GHAPP_OWNER` | Organization or user installation owner. |
| `GHAPP_REPOSITORY` | Repository selector in `OWNER/REPO` or `HOST/OWNER/REPO` form. |
| `GHAPP_HOST` | GitHub hostname; defaults to `github.com`. |
| `GHAPP_API_URL` | REST API base URL. |
| `GHAPP_API_VERSION` | REST API version; defaults to `2026-03-10`. |
| `GHAPP_CACHE_DIR` | Installation-token cache directory. |
| `GHAPP_NO_CACHE` | Disable token caching when truthy. |
| `GHAPP_REFRESH_BEFORE` | Refresh threshold, such as `5m`. |
| `GHAPP_HTTP_TIMEOUT` | API and broker timeout, such as `30s`. |
| `GHAPP_REAL_GH` | Underlying `gh` executable or name. |
| `GHAPP_REAL_GIT` | Underlying Git executable or name. |

Equivalent global flags are accepted before the `ghapp` subcommand. Run `ghapp help` for the concise reference.

## Token cache

Installation tokens are cached in the user cache directory by default. Cache directories are required to be real directories rather than symbolic links and are restricted to the current user where the platform permits it. Cache files use mode `0600` on Unix-like systems.

The cache uses an inter-process lock to prevent parallel token minting. Corrupt, unreadable, or expired entries are treated as misses and replaced. If a safe user cache directory cannot be determined, caching is disabled rather than falling back to a shared temporary directory.

## Security considerations

- Tokens are not placed in command-line arguments or repository URLs.
- The App private key is retained by the top-level broker and stripped from child environments.
- Installation tokens supplied to `gh` are visible to that child process and processes with permission to inspect it.
- Descendants of `ghapp exec`, Git hooks, and extensions inherit the broker capability so they can perform transparent nested operations. Treat arbitrary wrapped commands as trusted for the lifetime of that invocation.
- Cached installation tokens remain sensitive until expiry.
- The wrapper cannot exceed App permissions, repository selection, organization policy, or endpoint support for installation tokens.
- Git commit author and committer identity are not changed by authentication.

Use operating-system process isolation, a secret manager, least-privilege App permissions, repository selection, and private-key rotation appropriate to the deployment.

## Development and releases

```sh
make check
VERSION=0.3.0 make release
```

The implementation uses only the Go standard library. CI runs tests on Linux, macOS, and Windows, runs the race detector and `go vet`, and cross-compiles the supported release targets. Pushing a `v*` tag creates a GitHub release with binaries and SHA-256 checksums.

## References

- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app>
- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation>
- <https://cli.github.com/manual/gh_help_environment>
- <https://git-scm.com/docs/gitcredentials>

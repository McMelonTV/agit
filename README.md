# viagh

`viagh` is a Go wrapper around GitHub CLI (`gh`) and Git. It authenticates commands with short-lived GitHub App installation tokens while retaining familiar `gh` and `git` command lines.

It is intended for CI workers, service accounts, build hosts, and local automation that should operate as a GitHub App rather than as a human user.

## Key behavior

- Repository- and owner-scoped `gh` commands automatically select the matching App installation.
- A bare `gh repo list` fans out across all App installations.
- Git authentication is lazy and restricted to the configured GitHub HTTPS host.
- GitHub SSH, SSH-over-port-443, `git://`, and insecure HTTP remotes are rewritten to HTTPS only in the wrapped process.
- Nested commands can switch installations through a process-scoped credential broker.
- App private-key material is not inherited by `gh`, Git hooks, extensions, or commands launched through `viagh exec`.
- Commands for a different GitHub host retain their existing user authentication and credential helpers.
- Commit authorship defaults to the GitHub App bot and can instead use a configured identity or add the bot as a co-author.

A GitHub App token is not a user token. The App's installation scope and permissions still determine which repositories and APIs are available, and activity is attributed to the App.

## Requirements

- Git.
- GitHub CLI for `gh` wrapping.
- A GitHub App installed on the accounts or organizations to access.
- The App's private key.
- Repository **Contents** permission for HTTPS Git operations.

Building from source requires Go 1.25 or newer. CI tests Go 1.25.12 and 1.26.5; release binaries are built with Go 1.26.5.

## Build

```sh
go build -trimpath -o viagh ./cmd/viagh
```

After the repository is published under its final `McMelonTV/viagh` name:

```sh
go install github.com/McMelonTV/viagh/cmd/viagh@latest
```

Or:

```sh
make check
make build
```

## Configure

At minimum, provide the App ID and private key:

```sh
export VIAGH_APP_ID=123456
export VIAGH_PRIVATE_KEY=/secure/path/app.private-key.pem
```

The App ID may also be supplied as `GITHUB_APP_ID`. Private-key alternatives are:

```sh
export VIAGH_PRIVATE_KEY_PEM="$(cat /secure/path/app.private-key.pem)"
export VIAGH_PRIVATE_KEY_BASE64="$(base64 < /secure/path/app.private-key.pem | tr -d '\n')"
```

Selection is usually inferred from the command or current repository. It can be constrained explicitly:

```sh
export VIAGH_INSTALLATION_ID=9876543
# or
export VIAGH_OWNER=acme
# or
export VIAGH_REPOSITORY=acme/widgets
```

For GitHub Enterprise Server:

```sh
export VIAGH_HOST=github.example.com
export VIAGH_API_URL=https://github.example.com/api/v3
```

`VIAGH_API_URL` must be an absolute HTTPS URL. Plain HTTP is accepted only for loopback addresses, which is useful for tests.

### Environment file

`viagh` also loads variables from an environment file at every invocation, including shim, `exec`, and credential-helper runs:

- Linux/macOS: `$XDG_CONFIG_HOME/viagh/viagh.env`, usually `~/.config/viagh/viagh.env`
- Windows: `%AppData%\viagh\viagh.env`

Set `VIAGH_CONFIG_FILE` to load from another path. A template is provided at `.config/viagh/viagh.env` in this repository.

File rules:

- Lines are `KEY=VALUE`, optionally prefixed with `export`. Blank lines and `#` comments are skipped.
- Variables already present in the process environment take precedence over the file.
- Empty values are ignored.
- Values are literal: no shell expansion, command substitution, or variable interpolation is performed. Quote values containing spaces, for example `VIAGH_GIT_NAME='Release Agent'`. Prefer `VIAGH_PRIVATE_KEY` paths or `VIAGH_PRIVATE_KEY_BASE64` over inline PEMs, which span multiple lines.
- A malformed file is an error and aborts the invocation.

### Git authorship

Wrapped commit-producing Git commands override repository, global, environment, and command-scope `user.name`/`user.email` configuration by default.

The default mode is App bot authorship. The configured name and email default to empty:

```sh
export VIAGH_GIT_AUTHORSHIP=bot
```

Available modes are:

| Mode | Result |
| --- | --- |
| `bot` | The GitHub App bot is used for new commit authorship and committer identity. This is the default. |
| `configured` | `VIAGH_GIT_NAME` and `VIAGH_GIT_EMAIL` are used for new commit authorship and committer identity. |
| `both` | The configured identity is used for new commits, and the App bot is added with a `Co-authored-by` trailer. |

For example:

```sh
export VIAGH_GIT_NAME='Release Agent'
export VIAGH_GIT_EMAIL='release-agent@example.com'
export VIAGH_GIT_AUTHORSHIP=both
```

If either configured identity field is empty, `configured` and `both` fall back to `bot`. Existing Git identity configuration can be preserved explicitly:

```sh
export VIAGH_OVERRIDE_GIT_IDENTITY=false
# or
viagh --override-git-identity=false git commit -m 'Use client identity'
```

The bot login is derived from the authenticated App. `viagh` uses the bot account's numeric GitHub no-reply address when available and a deterministic no-ID bot address as a compatibility fallback. In `both` mode, existing Git hooks run before `viagh` appends one idempotent bot co-author trailer. Commit-producing Git aliases are treated conservatively and receive the same identity controls.

History-preserving operations such as cherry-pick, rebase, and applying patches may retain the original commit author by Git design. `viagh` still overrides the committer identity for the new commit. External `git-*` executables invoked directly are outside command classification; run them through `viagh exec` or configure their identity explicitly when they create commits.

### OpenCode attribution trailer

Set `VIAGH_OPENCODE_TRAILER=1` (or `--opencode-trailer`) to append a trailer identifying the opencode model and version that produced the commit. This is independent of Git authorship mode and applies to commit-producing commands whenever `viagh` runs as an opencode subprocess.

```sh
export VIAGH_OPENCODE_TRAILER=1
viagh git commit -m 'fix: handle empty input'
```

The trailer is discovered from the local opencode server rather than from environment variables. When `OPENCODE=1` and `OPENCODE_PORT` are set, `viagh` queries the server for the session most recently active in the repository and records its model, reasoning variant, and opencode version:

```
Worked on by `opencode-go/deepseek-v4-flash:max` within `opencode 1.18.13`.
```

The reasoning variant is included only when it is not `default`. Discovery is best-effort: an unreachable server, missing credentials, or no matching session silently skips the trailer without failing the commit. The trailer is added before the `Co-authored-by` trailer and is idempotent on re-runs.

## Explicit wrapper mode

```sh
viagh gh pr list --repo acme/widgets
viagh gh project list --owner acme
viagh gh secret list --org acme
viagh gh api /orgs/acme/repos

viagh git clone git@github.com:acme/widgets.git
viagh git fetch --all
viagh git push origin main
```

Run an arbitrary command with process-local `gh` and `git` shims:

```sh
viagh exec -- make release
```

Nested calls can use different installations:

```sh
viagh exec -- sh -c '
  gh repo view acme/one
  gh repo view beta/two
  git clone git@github.com:acme/three.git
'
```

Other utility commands:

```sh
viagh token
viagh env VIAGH_APP_ID
viagh installations
viagh doctor
viagh version
```

`viagh token` prints a token and should be used only where exposing a token through an environment variable is acceptable.

`viagh env KEY` prints the value of a variable and where it came from, `env` (process environment) or `env_file` (the environment file). It exits nonzero when the variable is not set:

## Transparent shim mode

The same executable can be invoked through links named `gh` and `git`.

```sh
export VIAGH_REAL_GH="$(command -v gh)"
export VIAGH_REAL_GIT="$(command -v git)"

install -m 0755 ./viagh "$HOME/.local/bin/viagh"
mkdir -p "$HOME/.local/libexec/viagh-shims"
ln -sfn "$HOME/.local/bin/viagh" "$HOME/.local/libexec/viagh-shims/gh"
ln -sfn "$HOME/.local/bin/viagh" "$HOME/.local/libexec/viagh-shims/git"
export PATH="$HOME/.local/libexec/viagh-shims:$PATH"
```

Commands then use App authentication without changing their syntax:

```sh
gh pr list --repo acme/widgets
gh repo list acme
git fetch origin
git push origin main
```

### T3 Code authentication discovery

T3 Code discovers GitHub CLI authentication by running:

```sh
gh auth status --json hosts
```

When `gh` is the viagh shim, that machine-readable probe returns the same
`hosts`/account shape as GitHub CLI after validating the GitHub App identity.
The reported active account is the App bot. The probe does not select an
installation or mint an installation token, so it remains valid when the App
is installed across multiple users or organizations.

Only the unformatted `--json hosts` status probe is intercepted. Interactive
auth management, token display, `--jq`, and `--template` continue to use the
underlying GitHub CLI credential store.

`VIAGH_REAL_GH` and `VIAGH_REAL_GIT` are recommended for deterministic CI and service environments. Without them, the wrapper finds the next executable on `PATH` while skipping itself.

## Installation selection

For authenticated `gh` operations, selection follows this order:

1. `VIAGH_INSTALLATION_ID` or `--installation-id`.
2. An explicit `--repo`/`-R` repository.
3. An owner inferred from commands such as `gh repo list OWNER`, `gh project list --owner`, `gh secret list --org`, repository creation, or an owner-bearing REST path.
4. A positional repository accepted by the relevant `gh repo` command.
5. `VIAGH_REPOSITORY`, `GH_REPO`, or `--repository`.
6. The current repository selected using GitHub CLI-compatible remote preferences.
7. `VIAGH_OWNER` or `--owner`.
8. The App's only installation.

For Git, the credential helper uses the exact HTTPS host and repository path supplied by Git. If no repository can be determined, an explicit owner or installation ID is required.

If multiple installations remain and no target can be inferred, `viagh` returns an error rather than choosing one arbitrarily.

## Multi-installation `gh repo list`

An ownerless repository list deliberately fans out across all installations:

```sh
viagh gh repo list --limit 100
viagh gh repo list --limit 100 --json nameWithOwner,isPrivate
```

Behavior across multiple installations:

- Installations are processed deterministically by account name.
- `--limit` is a global result limit, not a per-installation limit.
- Plain output is buffered and emitted only after every required invocation succeeds.
- Unformatted `--json` arrays are merged into one valid JSON array; no results produces `[]`.
- `--jq` and `--template` are rejected because applying them separately would produce misleading aggregate semantics. Specify an owner, or request merged JSON and format it afterward.
- `VIAGH_INSTALLATION_ID`, `VIAGH_OWNER`, or `VIAGH_REPOSITORY` restricts fan-out to the selected installation.

The combined plain result is grouped in installation account order; GitHub does not provide a cross-installation server-side ordering operation.

## Git authentication design

Wrapped Git commands receive process-local Git configuration that:

- registers `viagh credential-helper` only for `https://<VIAGH_HOST>`;
- enables `credential.useHttpPath` for repository-specific installation selection;
- clears conflicting authorization headers only for that host;
- rewrites supported GitHub remote schemes to HTTPS without changing repository configuration.

The helper returns no credential for another host or protocol, leaving unrelated GitLab, Bitbucket, Enterprise, and private-host credentials untouched.

A loopback broker owned by the top-level `viagh` process holds the App credentials and mints installation tokens on demand. Nested wrappers receive only a random, process-scoped broker capability and a configuration record with private-key fields removed. The broker shuts down when the wrapped command exits.

## Pass-through behavior

Authentication is skipped for local or informational `gh` operations such as help, version, completion, configuration, alias management, and authentication inspection.

A command targeting a host other than `VIAGH_HOST` is passed to the real `gh` without the App token or broker environment. This applies to explicit host/repository arguments, `GH_HOST`, and a current checkout whose selected remote belongs to another host.

## Configuration reference

| Variable | Purpose |
| --- | --- |
| `VIAGH_CONFIG_FILE` | Path to an environment file to load at startup; defaults to `$XDG_CONFIG_HOME/viagh/viagh.env`. |
| `VIAGH_APP_ID` | App ID or client ID used as the JWT issuer. |
| `VIAGH_PRIVATE_KEY` | Path to the private-key PEM. |
| `VIAGH_PRIVATE_KEY_PEM` | Inline private-key PEM. |
| `VIAGH_PRIVATE_KEY_BASE64` | Base64-encoded private-key PEM. |
| `VIAGH_INSTALLATION_ID` | Explicit installation ID. |
| `VIAGH_OWNER` | Organization or user installation owner. |
| `VIAGH_REPOSITORY` | Repository selector in `OWNER/REPO` or `HOST/OWNER/REPO` form. |
| `VIAGH_HOST` | GitHub hostname; defaults to `github.com`. |
| `VIAGH_API_URL` | REST API base URL. |
| `VIAGH_API_VERSION` | REST API version; defaults to `2026-03-10`. |
| `VIAGH_CACHE_DIR` | Installation-token cache directory. |
| `VIAGH_NO_CACHE` | Disable token caching when truthy. |
| `VIAGH_REFRESH_BEFORE` | Refresh threshold, such as `5m`. |
| `VIAGH_HTTP_TIMEOUT` | API and broker timeout, such as `30s`. |
| `VIAGH_REAL_GH` | Underlying `gh` executable or name. |
| `VIAGH_REAL_GIT` | Underlying Git executable or name. |
| `VIAGH_GIT_NAME` | Name for `configured` or `both` Git authorship; defaults to empty. |
| `VIAGH_GIT_EMAIL` | Email for `configured` or `both` Git authorship; defaults to empty. |
| `VIAGH_GIT_AUTHORSHIP` | `bot`, `configured`, or `both`; defaults to `bot`. |
| `VIAGH_OVERRIDE_GIT_IDENTITY` | Override existing Git identity when true; defaults to true. Set false to preserve client configuration. |
| `VIAGH_OPENCODE_TRAILER` | Append an opencode model attribution trailer to commit-producing Git commands when running as an opencode subprocess; defaults to false. |

Equivalent global flags are accepted before the `viagh` subcommand. Run `viagh help` for the concise reference.

## Token cache

Installation tokens are cached in the user cache directory by default. Cache directories are required to be real directories rather than symbolic links and are restricted to the current user where the platform permits it. Cache files use mode `0600` on Unix-like systems.

The cache uses an inter-process lock to prevent parallel token minting. Corrupt, unreadable, or expired entries are treated as misses and replaced. If a safe user cache directory cannot be determined, caching is disabled rather than falling back to a shared temporary directory.

## Security considerations

- Tokens are not placed in command-line arguments or repository URLs.
- The App private key is retained by the top-level broker and stripped from child environments.
- Installation tokens supplied to `gh` are visible to that child process and processes with permission to inspect it.
- Descendants of `viagh exec`, Git hooks, and extensions inherit the broker capability so they can perform transparent nested operations. Treat arbitrary wrapped commands as trusted for the lifetime of that invocation.
- Cached installation tokens remain sensitive until expiry.
- The wrapper cannot exceed App permissions, repository selection, organization policy, or endpoint support for installation tokens.
- Git identity is overridden for recognized commit-producing commands and configured Git aliases unless `VIAGH_OVERRIDE_GIT_IDENTITY=false` is set. History-preserving operations may retain the original author while using the selected viagh identity as committer.

Use operating-system process isolation, a secret manager, least-privilege App permissions, repository selection, and private-key rotation appropriate to the deployment.

## Development and releases

```sh
make check
VERSION=0.3.0 make release
```

The implementation uses only the Go standard library. CI runs tests on Linux, macOS, and Windows, checks the supported Go 1.25 baseline, runs the race detector and `go vet`, and cross-compiles the supported release targets. Pushing a `v*` tag creates a GitHub release containing `.tar.gz` archives for Linux/macOS, `.zip` archives for Windows, and SHA-256 checksums. Release targets are amd64 and arm64 for all three operating systems.

## References

- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app>
- <https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation>
- <https://cli.github.com/manual/gh_help_environment>
- <https://git-scm.com/docs/gitcredentials>

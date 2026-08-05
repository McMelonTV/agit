# Changelog

## 0.3.0

- Rename the project, executable, module, environment namespace, runtime identifiers, and release artifacts to `viagh`.
- Replace global `GIT_ASKPASS` authentication with a host-scoped Git credential helper.
- Refuse to provide App credentials to non-matching protocols or GitHub hosts.
- Add a loopback credential broker so nested `gh`, Git, and `exec` commands can select different installations without inheriting the App private key.
- Preserve user authentication when a command explicitly or implicitly targets another GitHub host.
- Propagate CLI-only configuration to lazy Git authentication without exposing private-key material to child processes.
- Clear stale wrapper tokens and Git configuration from nested and pass-through invocations.
- Route repository creation, edit, archive, unarchive, fork, sync, view, and explicit `--repo` commands correctly.
- Bypass App authentication for help, version, completion, configuration, and other local-only `gh` operations.
- Make ownerless multi-installation `gh repo list --limit` global, emit `[]` for empty JSON results, buffer output until all installations succeed, and reject ambiguous cross-installation `--jq` or `--template` formatting.
- Add signal forwarding and conventional signal-derived exit codes.
- Add inter-process token-cache locking, tolerant cache recovery, stricter cache-directory handling, and stricter API URL validation.
- Recognize GitHub SSH-over-port-443 remotes and richer current-repository remote selection.
- Add Windows path lookup fixes and cross-platform CI/release builds using supported Go toolchains.
- Remove generated binaries from source control.
- Add configurable Git authorship modes for App bot, configured identity, or configured author plus App bot co-author.
- Default commit identity to the App bot, fall back to it when configured name or email is empty, and allow identity overriding to be explicitly disabled.
- Preserve existing Git hooks while injecting bot co-author trailers for commit-producing workflows.
- Apply identity controls conservatively to configured Git aliases, run existing commit-message hooks before adding the bot trailer, and avoid duplicate `commit-tree` trailers.
- Report the GitHub App bot through `gh auth status --json hosts` for T3 Code and other GitHub CLI discovery clients without selecting an installation.
- Package release binaries in executable-preserving archives for Linux, macOS, and Windows on amd64 and arm64.
- Set Go 1.25 as the supported source-build baseline and reduce the CI matrix while retaining cross-platform and old-stable coverage.

## 0.2.0

- Route owner-scoped `gh` commands to the matching GitHub App installation.
- Recognize `gh repo list OWNER`, common owner flags, and owner-bearing REST API paths.
- Fan an ownerless `gh repo list` out across all App installations.
- Merge unformatted `gh repo list --json` results into one JSON array.
- Honor explicit installation, owner, and repository selectors for ownerless repository lists.
- Paginate App installation discovery beyond 100 installations.

## 0.1.0

- Initial transparent `gh` and Git wrapper with GitHub App installation-token authentication.

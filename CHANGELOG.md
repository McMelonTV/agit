# Changelog

## 0.3.0

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

## 0.2.0

- Route owner-scoped `gh` commands to the matching GitHub App installation.
- Recognize `gh repo list OWNER`, common owner flags, and owner-bearing REST API paths.
- Fan an ownerless `gh repo list` out across all App installations.
- Merge unformatted `gh repo list --json` results into one JSON array.
- Honor explicit installation, owner, and repository selectors for ownerless repository lists.
- Paginate App installation discovery beyond 100 installations.

## 0.1.0

- Initial transparent `gh` and Git wrapper with GitHub App installation-token authentication.

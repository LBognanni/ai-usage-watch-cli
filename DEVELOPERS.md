# Developing aiu

For installation, account setup, and output examples, see [README.md](README.md).

## Scope

Linux-only, standard-library Go with zero external Go dependencies. The CLI reads server-reported subscription quotas for Claude, OpenAI/Codex, and GitHub Copilot. No inference calls, token pricing, session scanning, historical database, daemon, or dashboard.

## Build and test

Docker (or Podman) and Make are the canonical build path. The Dockerfile pins Go 1.25.3 and disables CGO, producing a standalone static executable for the container's Linux architecture.

```sh
make build                         # dist/aiu
make test
make fmt
make check                         # formatting, go vet, tests
make build CONTAINER=podman
make build VERSION=v0.1.0          # embed --version without creating a tag
```

Containers mount only the repository and a build cache, run as the host UID/GID, and do not receive provider credentials. The build cache defaults to `${XDG_CACHE_HOME:-~/.cache}/aiu-build`; override `CACHE_DIR`, `CONTAINER`, or `IMAGE` through Make variables.

With a local Go toolchain:

```sh
go test ./...
go test -race ./...                 # requires CGO and a C compiler
go vet ./...
CGO_ENABLED=0 go build -o dist/aiu ./cmd/aiu
```

### Optional devcontainer

In VS Code, choose **Dev Containers: Reopen in Container**. `.devcontainer/devcontainer.json` uses the Dockerfile's `dev` target with the Go extension. Run Go commands directly inside it; run the resulting executable on the host, where client logins are available. No credential mounts are configured.

### CI

`.github/workflows/check.yml` lints workflows with actionlint and the installer with ShellCheck, runs `make check`, builds the executable, and checks `--version` on pushes and pull requests. Tests use synthetic credentials and quota responses; live accounts are not required. Installer integration tests fake downloads, exercise piped stdin, and cover architectures, version selection, checksum failures, cleanup, and preservation of existing installations.

## Releases

Once `.github/workflows/release.yml` is on `main`, open **Actions → Release → Run workflow**, select `main`, and enter a stable version such as `0.1.0` or `v0.1.0`.

The workflow:

1. Validates `main` and the version, refusing existing tags.
2. Checks formatting/shell syntax, runs `go vet` and race-enabled tests.
3. Cross-compiles static Linux amd64 and arm64 binaries using the pinned Go toolchain, embedding the tag in `aiu --version`.
4. Creates `aiu_linux_amd64.tar.gz`, `aiu_linux_arm64.tar.gz`, and `SHA256SUMS`.
5. Creates and pushes an annotated `vX.Y.Z` tag for the tested commit.
6. Uploads assets to a draft release with generated notes, then publishes it as the latest release.

Releases are serialized. Only the built-in `GITHUB_TOKEN` is needed, with `contents: write`; repository rules must permit the workflow to push version tags and publish releases. Pushing a tag alone does not run this workflow, and it never pushes changes to `main`.

Build/test failures happen before tagging. If publishing fails after tag creation, a tag and/or draft release can remain. Inspect them and finish publication manually; don't overwrite an existing version with different binaries.

Local builds default to `aiu dev`. Use `make build VERSION=vX.Y.Z` or `docker build --target binary --build-arg VERSION=vX.Y.Z ...` to embed a version without creating or publishing a release.

### Installer

`install.sh` is POSIX shell, designed for `curl | sh`. It detects Linux x86_64/arm64, downloads archives and `SHA256SUMS` over HTTPS, verifies the selected archive before extraction, and atomically replaces the executable in the installation directory. Failures preserve the existing executable; temporary directories are cleaned up. No client credentials are read.

Latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/LBognanni/ai-usage-watch-cli/main/install.sh | sh
```

Pinned version or custom destination:

```sh
curl -fsSL https://raw.githubusercontent.com/LBognanni/ai-usage-watch-cli/main/install.sh | AIU_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/LBognanni/ai-usage-watch-cli/main/install.sh | AIU_INSTALL_DIR="$HOME/bin" sh
```

The checksum protects download integrity, not against a compromised release publisher; users must trust this repository. Because latest archive/checksum downloads are separate requests, a concurrent new release can cause a safe verification failure—retry or pin a version. The public installer URL requires these files to be pushed and at least one release to be published.

## Code layout

| Path | Responsibility |
| --- | --- |
| `cmd/aiu/` | Flags, timeouts, watch loop, signals, cache coordination |
| `internal/credentials/` | Read-only Claude Code and OpenCode credential readers |
| `internal/provider/` | HTTP requests and provider-specific response decoders |
| `internal/quota/` | Secret-free normalized snapshot model and freshness rules |
| `internal/cache/` | Atomic latest-snapshot writes and cross-process refresh lock |
| `internal/engine/` | Parallel fetching, independent failures, stale fallback |
| `internal/ui/` | Normal, compact, and status rendering; shared bar palette |

Keep provider schemas isolated and defensively parsed. Missing windows must never become confident zero-usage readings. Test schema variations, missing windows, reset expiration, stale fallback, and account switches when changing adapters.

## Credentials and provider endpoints

These are **private/internal, unsupported endpoints** and can change without notice:

| Provider | Credential source | Read-only endpoint |
| --- | --- | --- |
| Claude | `~/.claude/.credentials.json` | `https://api.anthropic.com/api/oauth/usage` |
| OpenAI | Active OpenCode ChatGPT OAuth account | `https://chatgpt.com/backend-api/wham/usage` |
| Copilot | Active OpenCode Copilot OAuth account | `https://api.github.com/copilot_internal/user` |

Claude's credential path respects `CLAUDE_CONFIG_DIR`. The reader rejects symlinks, oversized/non-regular files, and group/world-readable permissions. A subscription OAuth login is required.

OpenAI/Copilot credentials are obtained through `opencode api get /api/credential`, not direct SQLite reads. The command may start OpenCode's local service. Select only active OAuth accounts, keep output in memory, and discard unrelated credentials. OpenAI needs the account ID from credential metadata. Enterprise-hosted Copilot credentials must never be sent to public GitHub; v1 rejects them.

**Never refresh, rotate, or write client credentials.** Claude Code and OpenCode own token lifecycle. Use that provider in its owning client after expiration; merely opening the client may not trigger refresh.

Do not put secrets in command arguments, logs, diagnostics, or the cache. Credential command stderr and raw provider errors are suppressed. HTTP redirects are not followed, preventing authorization from reaching another host. Label rendering strips terminal controls and bidi formatting.

OpenAI's windows are identified by duration, not primary/secondary order. Its endpoint measures Codex allowances, not general ChatGPT chat limits. Copilot quota access depends on OAuth scope and company policy; unsupported responses fail explicitly rather than inventing usage.

## Cache and freshness

Latest snapshot: `${XDG_CACHE_HOME:-~/.cache}/aiu/snapshot.json`.

- Versioned JSON containing normalized percentages, reset times, plan/local labels, status, timestamps, and a one-way identity hash. No tokens, raw account IDs, emails, or raw provider responses.
- Atomic writes with owner-only permissions. A neighboring `.lock` file serializes refreshes to avoid prompt stampedes and lost updates across provider selections.
- Lock waiting counts toward the refresh timeout. Fresh status reads don't lock or rewrite the cache.
- Preserve successful readings and their original timestamps through failures. Drop account-specific cached data after a known identity switch.
- HTTP 429 respects `Retry-After`, otherwise backs off for 5 minutes. Other failures receive a short cooldown; disconnected providers use the maximum cache age.
- Past reset times invalidate windows. Do not assume a reset means a fresh zero reading.
- A fresh cached status cannot detect an account switch until the next credential lookup.

## CLI behavior and rendering

Default reads and watch refreshes have a 10-second overall timeout. `--status` defaults to 2 seconds and refreshes only stale data. `--cached` performs no credential or network calls.

```sh
aiu --watch --interval 10m
aiu --status --max-age 10m --timeout 1s
```

Watch repaints once per second in a terminal but fetches only on its network interval (minimum 1 minute). Redirected watch output emits a snapshot per refresh without screen clearing. Ctrl-C restores the cursor and exits cleanly.

Normal and compact views share one truecolor bar renderer and exact RGB palette: lime below 70%, yellow from 70%, red from 90%, with matching dark tints for unfilled cells. Compact countdowns overlay the underlying cell backgrounds, including across the fill boundary. `--no-color` and `NO_COLOR` select the `█`/`░` character fallback.

Normal output includes all recognized windows, plan/local account labels, failure details, and an oldest-reading footer. Compact output includes only provider/plan names and the main bars, without brackets or footer; `?` means unknown/elapsed, `~` means stale. Compact mode cannot be combined with `--status`.

Reset dates use local time. Copilot date-only monthly resets are midnight UTC. Compact countdowns use `H:MM` below 24 hours and `Dd Hh` otherwise. Status pairs are 5-hour/weekly percentages used, with one monthly percentage for Copilot.

Exit codes: `0` for successful output, including provider outages; `1` for local setup errors; `2` for invalid arguments. This keeps status commands safe for shell prompts.

Example tmux integration:

```tmux
set -g status-interval 30
set -g status-right '#(aiu --status)'
```

## Credits

Provider adapters were informed by [CodeBurn](https://github.com/getagentseal/codeburn)'s live-quota approach. This implementation is purpose-built Go rather than a fork of its broader usage-accounting system.

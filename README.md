# aiu

A small Linux CLI for **Claude Pro**, **ChatGPT Plus/Codex**, and **GitHub Copilot** subscription quotas. See percentage **used**, reset times, and plan/account labels. No token pricing, session scanning, daemon, or dashboard.

## Build without installing Go

You need Docker (or Podman), Make, and permission to run containers:

```sh
make build
./dist/aiu
```

The build uses Go 1.25.3 in a container and produces a standalone, statically linked Linux executable. The repository and a build cache are mounted as your UID/GID; provider credentials are **not** mounted. Go is not needed to run the binary.

```sh
make test
make fmt
make check           # formatting, go vet, tests
make build CONTAINER=podman
```

Optional: open the repository in VS Code and choose **Dev Containers: Reopen in Container**. The devcontainer uses the same Dockerfile toolchain. Inside it, use `go test ./...` and `go build -o dist/aiu ./cmd/aiu` directly. Run `dist/aiu` on the host, where your client logins live; the devcontainer deliberately has no access to them.

To install the executable:

```sh
install -Dm755 dist/aiu ~/.local/bin/aiu
```

Ensure `~/.local/bin` is in `PATH`.

## Connect your accounts

- **Claude:** sign into Claude Code with your subscription (`claude auth login`). `aiu` reads `~/.claude/.credentials.json`, or `$CLAUDE_CONFIG_DIR/.credentials.json`. The file must have private permissions (`chmod 600`).
- **OpenAI:** connect OpenAI using ChatGPT OAuth in **OpenCode V2**, not an API key.
- **Copilot:** connect GitHub Copilot in **OpenCode V2**. Company-provided GitHub.com seats are supported when GitHub exposes their quota to that login. Enterprise-specific hosts are not supported in v1.

For OpenAI and Copilot, `opencode` must be on `PATH`. `aiu` invokes `opencode api get /api/credential`, selects active OAuth credentials, and keeps its output in memory. It never directly reads OpenCode's database. OpenCode may start its local service as part of that command.

`aiu` **never refreshes tokens or modifies client credentials**. If a login expires, use that provider in Claude Code/OpenCode to let the owning client refresh it, then retry. Merely opening a client may not refresh its token until you use it.

## Usage

```sh
aiu                                  # live read, quota bars
aiu --watch                          # refresh every 5 minutes; Ctrl-C exits
aiu --compact                        # provider/plan and countdown progress bars only
aiu --compact --watch                # compact live monitor
aiu --status                         # compact line, refresh only if stale
aiu --status --cached                 # instant cache-only output
aiu --provider claude,openai          # select providers
aiu --watch --interval 10m
aiu --status --max-age 10m --timeout 1s
aiu --no-color
aiu --help
```

Example (illustrative values):

```text
Claude · Pro · Claude Code
  5-hour                 ███████░░░░░░░░░░░░░  34.0% used  resets in 2h 18m
  Weekly                 ███████████░░░░░░░░░  53.0% used  resets Tue Nov 3 09:00 CET

OpenAI · Plus · default
  5-hour                 ███░░░░░░░░░░░░░░░░░  14.0% used  resets in 4h 06m
  Weekly                 ████████░░░░░░░░░░░░  39.0% used  resets Thu Nov 5 16:20 CET

updated 2m ago (oldest reading)
```

Compact view (`--compact`; countdowns are embedded in usage bars):

```text
Claude Pro        ███████░2:18░░░░░░░░ ███████3d 4h░░░░░░░░
OpenAI Plus       ███░░░░░4:06░░░░░░░░ ██████5d 11h░░░░░░░░
Copilot Business  █████░21d 0h░░░░░░░░
```

Compact mode shows **only provider/plan names and bars**: 5-hour then weekly for Claude/OpenAI, monthly for Copilot. Filled cells indicate percentage used; overlaid countdowns use `H:MM` below 24 hours and `Dd Hh` otherwise. Missing or elapsed windows show `?` inside an unfilled bar; `~` marks stale readings. Account labels, additional model windows, percentages, errors, timestamps, and watch instructions are omitted—use the normal view for details. `--compact` works with `--watch`, `--cached`, and provider selection; it cannot be combined with `--status`.

The normal bar view uses one freshness footer rather than repeating timestamps under healthy providers. Each provider retains its own timestamp internally; stale rows show their individual age and failure state. The footer refers to the oldest displayed reading, not the last refresh attempt.

Colored compact bars use solid truecolor backgrounds: lime below 70%, yellow from 70%, red from 90%, with a matching dark tint for the remaining portion. Text shares the exact background of the bar beneath it—there is no separate text box or shaded-glyph texture. They have no brackets. `--no-color` and `NO_COLOR` disable colors in both views; without color, compact bars use the `█`/`░` character overlay shown above instead.

Prompt/status output (`--status`):

```text
Claude 34/53% · OpenAI 14/39% · Copilot 24% · updated 2m ago
```

Claude/OpenAI pairs are **5-hour/weekly percentages used**. Copilot is its monthly allowance. `?` means that window was not reported or its last reading has elapsed, **not** zero usage. Additional model-scoped windows appear in the bar view.

Watch redraws once per second in a terminal, but fetches only on its network interval (minimum 1 minute). When redirected, it prints one snapshot per refresh without ANSI screen clearing. `NO_COLOR` disables colors. Reset dates use your local timezone; monthly date-only Copilot resets are interpreted as midnight UTC.

### Shell and tmux

Use `aiu --status` in your prompt's existing command hook. Fresh snapshots are read locally. A stale read may wait **up to 2 seconds** for refresh by default; use `--cached` if prompt latency must be zero. Normal live/watch reads have a **10-second** overall refresh timeout.

Example tmux configuration:

```tmux
set -g status-interval 30
set -g status-right '#(aiu --status)'
```

Provider outages do not fail the whole command. `~` marks stale values, accompanied by the failure state and reading age. Invalid arguments exit 2, local setup errors exit 1, and provider errors exit 0 so they do not break prompts.

## Cache and privacy

Only the latest normalized snapshot is stored at:

```text
${XDG_CACHE_HOME:-~/.cache}/aiu/snapshot.json
```

A neighboring `.lock` file serializes live refreshes across watch and prompt processes. Waiting for another refresh counts toward the command's timeout; it falls back to cached output rather than blocking indefinitely. Fresh status reads do not lock or rewrite the cache.

Writes are atomic with owner-only file permissions. The cache contains percentages, reset times, plan/local account labels, status, timestamps, and a one-way credential/account identity hash to avoid reusing quota across known account switches. It contains **no tokens, account IDs, email addresses, or raw provider responses**. This is a latest-reading cache, not usage history.

Failed refreshes retain the previous reading with its original timestamp. HTTP 429 respects `Retry-After` (otherwise 5 minutes); other failures get a short cooldown. No guessed usage is substituted for missing data. A cached fresh status cannot notice an account switch until its next credential lookup.

All quota calls go directly to the relevant provider. Authorization is never forwarded across redirects. No telemetry or third-party service is involved. Credentials are never placed in process arguments, stdout, or error messages.

## Limitations

All three quota APIs are **private/internal and unsupported**:

- Claude: `https://api.anthropic.com/api/oauth/usage`
- OpenAI/Codex: `https://chatgpt.com/backend-api/wham/usage`
- Copilot: `https://api.github.com/copilot_internal/user`

These expose server-reported subscription limits, not local session estimates. OpenAI's endpoint reports **Codex allowances**, not every model-specific ChatGPT chat limit. Windows are rendered only when supplied by the provider. Company policy, OAuth scope, rate limiting, or API changes can make Copilot data unavailable; `aiu` explains the failure and continues showing other providers.

Read-only credential reuse intentionally trades unattended operation for not interfering with the owning client's OAuth lifecycle. This tool does not bypass usage limits or perform inference calls.

Provider adapters were informed by [CodeBurn](https://github.com/getagentseal/codeburn)'s live-quota approach. The implementation here is purpose-built, standard-library Go with zero external Go dependencies.

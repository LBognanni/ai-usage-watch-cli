# aiu

A small Linux CLI to check your **Claude Pro**, **ChatGPT Plus/Codex**, and **GitHub Copilot** subscription limits. See how much you've used and when your allowance resets.

## What it looks like

**Normal view** — `aiu`:

```text
Claude · Pro · Claude Code
  5-hour                 ███████░░░░░░░░░░░░░  34.0% used  resets in 2h 18m
  Weekly                 ███████████░░░░░░░░░  53.0% used  resets Tue Nov 3 09:00 CET

OpenAI · Plus · default
  5-hour                 ███░░░░░░░░░░░░░░░░░  14.0% used  resets in 4h 06m
  Weekly                 ████████░░░░░░░░░░░░  39.0% used  resets Thu Nov 5 16:20 CET

updated 2m ago (oldest reading)
```

**Compact view** — `aiu --compact`:

```text
Claude Pro        ███████░2:18░░░░░░░░ ███████3d 4h░░░░░░░░
OpenAI Plus       ███░░░░░4:06░░░░░░░░ ██████5d 11h░░░░░░░░
Copilot Business  █████░21d 0h░░░░░░░░
```

Bars show usage consumed. Claude/OpenAI show **5-hour then weekly** limits; Copilot shows its **monthly** allowance. The text inside compact bars is the time until reset. In a color terminal, bars have solid colored backgrounds with matching dark unfilled regions.

**Prompt/status line** — `aiu --status`:

```text
Claude 34/53% · OpenAI 14/39% · Copilot 24% · updated 2m ago
```

Examples use illustrative values. `?` means unknown, not zero; `~` marks an older reading.

## Get started

Install the latest release (Linux x86_64 or arm64; requires `curl`, `tar`, and `sha256sum`):

```sh
curl -fsSL https://raw.githubusercontent.com/LBognanni/ai-usage-watch-cli/main/install.sh | sh
```

The installer verifies the download and installs to `~/.local/bin` without sudo. Ensure that directory is in your `PATH`. Run the same command to update. It becomes available once the first GitHub release is published.

Prefer not to pipe a script into your shell? Download a binary from [Releases](https://github.com/LBognanni/ai-usage-watch-cli/releases), or [build it yourself](DEVELOPERS.md#build-and-test).

Sign into your existing clients:

- **Claude:** use your subscription login in Claude Code (`claude auth login`).
- **OpenAI:** connect OpenAI with ChatGPT OAuth in **OpenCode V2**, not an API key.
- **Copilot:** connect GitHub Copilot in **OpenCode V2**. Company-provided GitHub.com seats work when company policy permits quota access; enterprise-specific hosts aren't supported.

Keep `opencode` in your `PATH`. If a login expires, use that provider in Claude Code or OpenCode to refresh it, then retry.

## Everyday use

```sh
aiu                         # check now
aiu --watch                 # refresh every 5 minutes; Ctrl-C exits
aiu --compact --watch       # minimal live monitor
aiu --provider claude,openai # show selected providers
aiu --status --cached       # instant output from the last reading
aiu --help                  # all options
```

`--status` uses fresh cached data, otherwise attempts a refresh for up to 2 seconds. Add `--cached` if your prompt must never wait. Use `--no-color` to disable colors.

## Good to know

- OpenAI usage means **Codex allowances**, not every ChatGPT chat-model limit.
- Provider errors don't hide the other providers. Use the normal view for details when compact mode shows `?` or `~`.
- Everything runs locally, with quota requests going directly to the providers. No telemetry; credentials are never modified or stored by `aiu`.
- Usage endpoints are unofficial and may change. Only the latest reading is cached, not your usage history.

Working on the project? See [DEVELOPERS.md](DEVELOPERS.md).

# cbl — Codex Bar for Linux

`cbl` is a small Go app for Ubuntu/Linux that reads your local Codex auth, calls the Codex usage endpoint, and exposes the remaining windows in a way that works well for bars and panels.

## What it does

- reads `~/.codex/auth.json` by default
- supports `tokens.access_token`, `tokens.refresh_token`, `tokens.account_id`
- supports `OPENAI_API_KEY` auth files too
- calls the current Codex usage endpoint:
  - `https://chatgpt.com/backend-api/wham/usage`
  - falls back to `/api/codex/usage` for non-backend API bases
- shows:
  - 5h window remaining
  - weekly window remaining
  - optional credit limit / remaining credits
  - extra model-specific windows when present
- outputs plain text, JSON, or Waybar JSON
- can run a small HTTP server for panel integrations

## Build

```bash
go build ./cmd/cbl
```

## Usage

### One-shot status

```bash
./cbl status
./cbl status --json
./cbl status --waybar
```

### Watch mode

```bash
./cbl watch --interval 5m --waybar
```

### Local server for bars

```bash
./cbl serve --addr 127.0.0.1:8088
curl http://127.0.0.1:8088/waybar
```

## Environment

- `CBL_AUTH_FILE` — override auth.json path
- `CBL_CONFIG_FILE` — override config.toml path
- `CBL_BASE_URL` — override the ChatGPT base URL
- `CBL_FIXTURE` — offline JSON fixture for testing

## Config file

If you need a custom base URL, use a small TOML file like:

```toml
chatgpt_base_url = "https://chatgpt.com/backend-api"
```

## Ubuntu integration ideas

### Waybar

Use the `/waybar` endpoint or `cbl status --waybar`.

### GNOME / Extension Manager

The cleanest pattern is:

1. run `cbl serve` in the background
2. let a thin GNOME Shell extension poll `http://127.0.0.1:8088/waybar`

That keeps the Go side focused on Codex logic and lets the shell extension stay tiny.

## Notes

This repo is intentionally Codex-only for now, per your request.

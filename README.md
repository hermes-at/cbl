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

## Install on Ubuntu

The repo now ships three Linux integrations:

1. **systemd --user** service for keeping `cbl serve` running
2. **GNOME Shell extension** for a top-bar status item
3. **AppIndicator / tray** helper for classic tray users

### One-command install

From a checked-out repo or unpacked release:

```bash
./install.sh
```

From the internet on a fresh machine:

```bash
curl -fsSL https://raw.githubusercontent.com/hermes-at/cbl/main/install.sh | bash
```

That installs `cbl`, starts the user service, installs the GNOME extension package, enables it when possible, and installs the tray helper.

If you want the explicit platform installer, `./install/ubuntu/install.sh --all` does the same thing.

### Ubuntu packages

For the tray helper build, install the native AppIndicator development package if it is missing:

```bash
sudo apt install libayatana-appindicator3-dev
```

### One-command uninstall

From a checked-out repo or unpacked release:

```bash
./uninstall.sh
```

From the internet on a fresh machine:

```bash
curl -fsSL https://raw.githubusercontent.com/hermes-at/cbl/main/uninstall.sh | bash
```

This removes the user service, extension, tray helper, autostart entry, and installed binaries.

### Package the GNOME extension for Extension Manager

```bash
./install/ubuntu/package-gnome-extension.sh
```

It produces `dist/cbl-gnome-extension.zip`, which you can load in Extension Manager.

### Release archives

Use `./release/package-release.sh` to build a zip and tarball suitable for GitHub Releases.

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
./cbl serve --addr 127.0.0.1:18088
curl http://127.0.0.1:18088/waybar
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

1. run `cbl serve` in the background via the systemd user service
2. let the GNOME Shell extension poll `http://127.0.0.1:18088/waybar`

On GNOME Shell 50, the extension is enabled by the installer when possible.
Package it with `./install/ubuntu/package-gnome-extension.sh` and install the zip in Extension Manager if you want manual control.

### AppIndicator / tray

The tray helper is `cbl-tray`.
It reads the same local server and can be autostarted from `~/.config/autostart/`.

## Notes

This repo is intentionally Codex-only for now, per your request.

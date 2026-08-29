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
- supports a simple GNOME profile config saved in `~/.config/cbl/config.json`

## Build

```bash
go build ./cmd/cbl
```

## Install on Ubuntu

The repo ships simple per-user Linux integrations:

1. **systemd --user** service for keeping `cbl serve` running
2. **AppIndicator / tray** helper for a visible desktop indicator
3. optional **GNOME Shell extension** for people who want to test it manually

### One-command install

From a checked-out repo or unpacked release:

```bash
./install.sh
```

From the internet on a fresh machine:

```bash
curl -fsSL https://raw.githubusercontent.com/hermes-at/cbl/main/install.sh | bash
```

That installs `cbl`, starts the user service, starts the tray helper for the current session, and adds the tray helper to autostart. It does **not** depend on the GNOME Shell extension path.

If you want the explicit platform installer, `./install/ubuntu/install.sh` does the same thing. To try the GNOME Shell extension too, run `./install.sh --all` or `./install.sh --extension`.

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

This stops/removes the user service, stops/removes the tray helper, removes optional extension files, removes the autostart entry, removes installed binaries, and deletes `~/.config/cbl`.

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

### GNOME / Extension Manager (optional)

The cleanest pattern is:

1. run `cbl serve` in the background via the systemd user service
2. let the GNOME Shell extension poll `http://127.0.0.1:18088/waybar`
3. use the extension's *Add / edit profile…* entry to store `~/.config/cbl/config.json`

The default installer intentionally skips the GNOME Shell extension and uses the tray helper. If you want to test the extension later, run `./install.sh --extension` or package it with `./install/ubuntu/package-gnome-extension.sh` and install the zip in Extension Manager.

### AppIndicator / tray

The tray helper is `cbl-tray`.
It reads the same local server and can be autostarted from `~/.config/autostart/`.

## Notes

This repo is intentionally Codex-only for now, per your request.

# CBL — Codex Bar for Linux

CBL is a standalone Linux desktop app for ChatGPT/Codex usage limits: a native GTK window, an AppIndicator panel icon, and a background Go service.
**CBL no longer uses a GNOME Shell extension.**

[Русская версия](README.ru.md)

## Components

- `cbl serve`: background service, account storage and usage refresh.
- `cbl-app`: standalone window with account cards, limits/reset times, device-code login and proxy settings.
- `cbl-tray`: panel indicator; opens the application window.
- Local HTTP and Waybar output remain available.

Closing the window does not stop the background service. Codex CLI is not required.

## Install / upgrade from this checkout

Standalone desktop is available in **v0.3.0**. To install/update from the published release:

```bash
curl -fsSL https://raw.githubusercontent.com/hermes-at/cbl/main/install.sh | bash
```

Ubuntu desktop runtime dependencies:

```bash
sudo apt install python3-gi gir1.2-gtk-3.0 libayatana-appindicator3-1
```

For source builds also install Go 1.23+, a C compiler, pkg-config and `libayatana-appindicator3-dev`.
Run as your regular desktop user, **not root**:

```bash
./install.sh
```

The installer sets up the window, user service and tray autostart. Legacy `--systemd --extension` is accepted as a migration alias but installs no CBL Shell extension. Upgrade removes old CBL extension files and **preserves `~/.config/cbl` accounts and configuration**. Do not uninstall before upgrading.

On GNOME the panel icon requires the desktop's AppIndicator support (Ubuntu AppIndicators, or AppIndicator/KStatusNotifierItem support on vanilla GNOME). That desktop integration is separate from CBL: no CBL-specific Shell extension is installed. The window works without panel support. GNOME may retain the old extension until logout/login.

Launch **CBL** from Applications or run:

```bash
~/.local/bin/cbl-app
systemctl --user status cbl.service
```

## Usage

- Open CBL from the panel menu or Applications.
- Refresh manually or let the service refresh automatically.
- Add an account, open the OpenAI confirmation page, copy the code and confirm login.
- Connection settings enable/disable proxying and save an HTTP(S) or SOCKS5 proxy URL.
- Failed accounts remain visible with an error instead of silently disappearing.

A disabled proxy means direct requests, including when `CBL_PROXY` exists in the service environment.

## Build and tests

```bash
make test
go vet ./...
make build-all
VERSION=v0.3.0 ./release/package-release.sh
```

GTK runtime smoke tests require a graphical session or an isolated virtual display. Unit tests do not require real OpenAI accounts.

## Local API and CLI

The service listens on `127.0.0.1:18088`. Do not expose account APIs to the network.

```bash
cbl status --json
cbl status --waybar
cbl serve --addr 127.0.0.1:18088
curl http://127.0.0.1:18088/api/status
curl http://127.0.0.1:18088/waybar
```

Desktop API: `GET /api/status`, `POST /api/refresh`, `GET/POST /api/settings`, `POST /api/login/start`, `POST /api/login/complete?id=...`.

## Files

- `~/.local/bin/cbl`, `cbl-tray`, `cbl-app` (and its Python module)
- `~/.local/share/applications/cbl.desktop`
- `~/.config/autostart/cbl-indicator.desktop`
- `~/.config/systemd/user/cbl.service`
- `~/.config/cbl/config.json`, `accounts/*.json`

Environment: `CBL_AUTH_FILE`, `CBL_CONFIG_FILE`, `CBL_BASE_URL`, `CBL_PROXY`, `CBL_FIXTURE`.

## Full uninstall

**This removes saved accounts and configuration too.** It is not needed for upgrades.

```bash
./uninstall.sh
```

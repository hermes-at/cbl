# CBL — Codex Bar for Linux

CBL — отдельная Linux-программа для просмотра лимитов ChatGPT/Codex: нативное GTK-окно, значок в панели через AppIndicator и фоновый Go-сервис.
**Собственного GNOME Shell extension больше нет.**

[English README](README.md)

## Компоненты

- `cbl serve` — хранение аккаунтов, получение лимитов, фоновое обновление.
- `cbl-app` — окно с карточками аккаунтов, лимитами и временем сброса, входом через device code и настройками прокси.
- `cbl-tray` — значок в панели и меню открытия приложения.
- HTTP API и Waybar остаются доступны.

Закрытие окна не останавливает сервис. Codex CLI не требуется.

## Установка / обновление из этой ветки

Standalone-приложение доступно в **v0.3.0**. Установка/обновление опубликованного релиза:

```bash
curl -fsSL https://raw.githubusercontent.com/hermes-at/cbl/main/install.sh | bash
```

Зависимости для Ubuntu:

```bash
sudo apt install python3-gi gir1.2-gtk-3.0 libayatana-appindicator3-1
```

Для сборки исходников также нужны Go 1.23+, C-компилятор, pkg-config и `libayatana-appindicator3-dev`.
Устанавливай **обычным пользователем рабочего стола, не root**:

```bash
./install.sh
```

Установщик добавляет окно, user service и автозапуск индикатора. Старые аргументы `--systemd --extension` принимаются для миграции, но CBL-расширение не устанавливается. Старые файлы расширения удаляются, **аккаунты и конфигурация `~/.config/cbl` сохраняются**. Перед обновлением не запускай uninstall.

Для значка в GNOME нужна поддержка AppIndicator самим рабочим столом: Ubuntu AppIndicators либо AppIndicator/KStatusNotifierItem на обычном GNOME. Это общая интеграция панели, не собственное расширение CBL. Окно программы работает и без неё. Старое расширение может оставаться в памяти GNOME до выхода/входа в сеанс.

Открывай **CBL** из меню приложений или запускай:

```bash
~/.local/bin/cbl-app
systemctl --user status cbl.service
```

## Использование

- Открой окно из меню панели или меню приложений.
- Обнови лимиты вручную либо дождись автоматического обновления сервисом.
- Добавь аккаунт: открой страницу OpenAI, скопируй код и подтверди вход.
- В настройках соединения включи/выключи прокси и укажи HTTP(S)/SOCKS5 URL.
- Недоступный аккаунт остаётся в списке с ошибкой, а не исчезает молча.

Отключённый прокси означает прямое соединение, даже если в окружении сервиса есть `CBL_PROXY`.

## Сборка и тесты

```bash
make test
go vet ./...
make build-all
VERSION=v0.3.0 ./release/package-release.sh
```

Для runtime-проверки GTK нужен графический сеанс либо изолированный виртуальный дисплей. Unit-тесты не требуют реальных аккаунтов OpenAI.

## CLI и API

Сервис слушает `127.0.0.1:18088`. Не публикуй API аккаунтов в сеть.

```bash
cbl status --json
cbl status --waybar
cbl serve --addr 127.0.0.1:18088
curl http://127.0.0.1:18088/api/status
curl http://127.0.0.1:18088/waybar
```

API приложения: `GET /api/status`, `POST /api/refresh`, `GET/POST /api/settings`, `POST /api/login/start`, `POST /api/login/complete?id=...`.

## Файлы

- `~/.local/bin/cbl`, `cbl-tray`, `cbl-app` и Python-модуль окна.
- `~/.local/share/applications/cbl.desktop`.
- `~/.config/autostart/cbl-indicator.desktop`.
- `~/.config/systemd/user/cbl.service`.
- `~/.config/cbl/config.json`, `accounts/*.json`.

Переменные: `CBL_AUTH_FILE`, `CBL_CONFIG_FILE`, `CBL_BASE_URL`, `CBL_PROXY`, `CBL_FIXTURE`.

## Полное удаление

**Удаляет в том числе сохранённые аккаунты и настройки.** Для обновления не нужно.

```bash
./uninstall.sh
```

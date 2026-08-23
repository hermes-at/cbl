#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
from pathlib import Path
from urllib.request import urlopen

URL = os.environ.get("CBL_WAYBAR_URL", "http://127.0.0.1:18088/waybar")


def fetch():
    with urlopen(URL, timeout=5) as resp:
        return json.loads(resp.read().decode())


def main() -> int:
    try:
        import gi
        gi.require_version("Gtk", "3.0")
        gi.require_version("AyatanaAppIndicator3", "0.1")
        from gi.repository import Gtk, GLib, AyatanaAppIndicator3 as AppIndicator3
    except Exception as exc:
        print(f"cbl tray unavailable: {exc}", file=sys.stderr)
        return 1

    indicator = AppIndicator3.Indicator.new(
        "cbl",
        "indicator-messages",
        AppIndicator3.IndicatorCategory.APPLICATION_STATUS,
    )
    indicator.set_status(AppIndicator3.IndicatorStatus.ACTIVE)

    menu = Gtk.Menu()
    title_item = Gtk.MenuItem(label="cbl starting…")
    title_item.set_sensitive(False)
    menu.append(title_item)
    menu.append(Gtk.SeparatorMenuItem())

    def refresh(*_args):
        try:
            payload = fetch()
            title_item.set_label(payload.get("text", "cbl"))
        except Exception as exc:
            title_item.set_label(f"cbl unavailable: {exc}")
        menu.show_all()
        return True

    refresh_item = Gtk.MenuItem(label="Refresh")
    refresh_item.connect("activate", refresh)
    menu.append(refresh_item)

    quit_item = Gtk.MenuItem(label="Quit")
    quit_item.connect("activate", lambda *_: Gtk.main_quit())
    menu.append(quit_item)

    menu.show_all()
    indicator.set_menu(menu)
    GLib.timeout_add_seconds(300, refresh)
    refresh()

    def stop(*_args):
        Gtk.main_quit()
        return False

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    Gtk.main()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

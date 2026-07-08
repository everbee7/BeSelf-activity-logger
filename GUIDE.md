# BeSelf Guide

## What BeSelf Does

BeSelf is a local Windows activity tracker. It records foreground app usage, idle time, locked time, and daily/weekly work statistics. Data stays on your machine in SQLite.

It does not record screenshots, keystrokes, mouse positions, accounts, cloud analytics, or sync data.

## Main Interfaces

- Native Windows dashboard: opens when the app starts.
- Tray icon: background controls.
- Web dashboard: available from the tray menu.

Tray menu:

- Open app: focuses the single native dashboard window.
- Open web dashboard: opens the local browser UI.
- Start tracking / Stop tracking: enables or disables data collection.
- Enable/Disable start with Windows: toggles startup registration.
- Exit: closes the background tracker.

Tray icon:

- Yellow: tracking is active.
- Gray: tracking is stopped.

## Dashboard Features

- Worked: active foreground-window time for the selected date.
- Week avg: average active time across the last 7 days ending on the selected date.
- Locked: Windows locked time.
- Idle: idle or unknown/no-window time.
- Weekly Statistics: daily active-time bars with time labels.
- Timeline: full-day timeline split into active, idle, locked, and unknown segments.
- Program Usage: apps used during the day or selected timeline range.

Timeline selection:

1. Drag across the timeline.
2. The selected period is highlighted.
3. Program Usage updates to only programs active during that period.

Program Usage:

- Scroll inside the Program Usage panel to see all programs.
- The app tries to show real executable icons.
- If a real icon cannot be extracted, it falls back to a generated initial icon.

## Data Storage

Database path:

```text
%AppData%\BeSelf\beself.db
```

Primary tables:

- `settings`: local app settings.
- `activity_sessions`: source activity records.
- `daily_summary`: rebuildable daily totals.

Activity session fields include:

- start/end time
- duration
- status
- process name
- window title
- executable path

## Install

Use the setup executable:

```text
installer\BeSelfSetup.exe
```

The installer copies the app to Program Files, adds Start Menu entries, optionally creates a desktop shortcut, and supports uninstall through Windows Apps settings.

## Development

Run from source:

```powershell
go run ./cmd/beself
```

Build executable:

```powershell
.\scripts\build.ps1
```

Build installer:

```powershell
& "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe" installer\BeSelf.iss
```

Run tests:

```powershell
go test ./...
```

## Architecture

- `cmd/beself`: app entrypoint and embedded dashboard assets.
- `internal/tracker`: tracking state machine.
- `internal/win`: Windows API calls for foreground window, idle, lock, and startup registration.
- `internal/store`: SQLite persistence and summary queries.
- `internal/tray`: system tray integration.
- `internal/nativeui`: custom native Windows dashboard.
- `internal/ui`: local HTTP API and web dashboard server.
- `installer`: Inno Setup installer script.

## Troubleshooting

If the app keeps running after closing the window, check the tray icon. BeSelf is designed to keep tracking in the background.

If tracking does not start, use the tray menu or native dashboard Start tracking button.

If the dashboard does not reflect tray start/stop immediately, it refreshes automatically every 2 seconds and can be reopened from the tray.

If icons look stale in the Windows taskbar, Windows may be caching them. Reinstalling from the latest setup or restarting Explorer usually refreshes the shell icon cache.

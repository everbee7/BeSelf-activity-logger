# BeSelf

BeSelf is a local-first Windows time tracker written in Go. It runs in the system tray, tracks foreground app usage, idle time, locked time, and opens a native Windows dashboard for reviewing daily activity. A browser dashboard is also available from the tray.

## Development

```powershell
go mod tidy
go run ./cmd/beself
```

The app opens the native dashboard on launch. From the tray menu, use `Open app` for the native Windows dashboard or `Open web dashboard` for the browser view.

## Build

```powershell
go build -ldflags="-H windowsgui -s -w" -o dist/BeSelf.exe ./cmd/beself
```

`cmd/beself/rsrc.syso` embeds the Windows manifest required by the native dashboard controls. If you change `cmd/beself/app.manifest`, regenerate it with:

```powershell
rsrc -manifest cmd/beself/app.manifest -o cmd/beself/rsrc.syso
```

## Installer

Install Inno Setup, then compile `installer/BeSelf.iss`. The installer includes the executable, creates Start Menu entries, and supports uninstall. Startup with Windows is controlled inside the app through the tray menu or dashboard.

## Privacy

All data stays on the local machine. BeSelf stores app/process name, window title, executable path, status, and timestamps. It does not capture screenshots, keystrokes, mouse coordinates, accounts, analytics, or cloud sync.

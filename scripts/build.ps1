$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

go build -ldflags="-H windowsgui -s -w" -o (Join-Path $dist "BeSelf.exe") ./cmd/beself

Write-Host "Built dist\BeSelf.exe"

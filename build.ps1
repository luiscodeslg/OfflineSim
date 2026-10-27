# Builds OfflineSim.exe for 64-bit Windows. Requires Go 1.25+ (winget install GoLang.Go).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$version = if ($env:VERSION) { $env:VERSION } else { "1.0.0" }
New-Item -ItemType Directory -Force dist | Out-Null
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -H windowsgui -X main.appVersion=$version" -o dist\OfflineSim.exe .
Write-Host "built dist\OfflineSim.exe"

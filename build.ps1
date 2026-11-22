<#
  HookTrap build script.
  Produces a single static hooktrap.exe and (if Inno Setup is present) a Windows installer.

  Usage:
    .\build.ps1                 # build exe into .\dist
    .\build.ps1 -Version 1.2.0  # stamp a version
    .\build.ps1 -Installer      # also compile the installer (needs ISCC on PATH or Inno Setup 6)
#>
[CmdletBinding()]
param(
  [string]$Version = "1.0.0",
  [switch]$Installer,
  [string]$ISCC = ""
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Locate the Go toolchain.
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { foreach ($c in @("$env:USERPROFILE\tools\go\bin\go.exe", "C:\Program Files\Go\bin\go.exe")) { if (Test-Path $c) { $go = $c; break } } }
if (-not $go) { throw "Go toolchain not found. Install Go 1.23+ and ensure 'go' is on PATH." }

$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force $dist | Out-Null

Write-Host "==> Building hooktrap.exe (windows/amd64) version $Version" -ForegroundColor Cyan
$env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = "amd64"
& $go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $dist "hooktrap.exe") ./cmd/hooktrap
if ($LASTEXITCODE -ne 0) { throw "go build failed" }
Write-Host "    -> $(Join-Path $dist 'hooktrap.exe')" -ForegroundColor Green

if ($Installer) {
  if (-not $ISCC) {
    $cands = @(
      (Get-Command iscc -ErrorAction SilentlyContinue).Source,
      "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
      "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
      "$env:USERPROFILE\tools\InnoSetup\ISCC.exe"
    ) | Where-Object { $_ -and (Test-Path $_) }
    if ($cands) { $ISCC = $cands[0] }
  }
  if (-not $ISCC) { throw "Inno Setup (ISCC.exe) not found. Pass -ISCC <path> or install Inno Setup 6." }
  Write-Host "==> Compiling installer with $ISCC" -ForegroundColor Cyan
  & $ISCC "/DAppVersion=$Version" (Join-Path $root "installer\hooktrap.iss")
  if ($LASTEXITCODE -ne 0) { throw "ISCC failed" }
  Write-Host "    -> installer in $dist" -ForegroundColor Green
}

Write-Host "Done." -ForegroundColor Cyan

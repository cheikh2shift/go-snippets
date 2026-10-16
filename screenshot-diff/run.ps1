$ErrorActionPreference = "Stop"
$PSScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location $PSScriptRoot

if (-not (Test-Path "go.mod")) {
  go mod init screenshot-diff
}

if (-not (Test-Path "go.sum")) {
  go get golang.org/x/image
}

if ($args.Count -lt 3) {
  Write-Host "Usage: .\run.ps1 <frame1.png> <frame2.png> <output.png>"
  exit 1
}

go run main.go $args[0] $args[1] $args[2]

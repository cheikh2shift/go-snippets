# One-shot setup + run for leakcheck (Windows / PowerShell).
#
#   .\run.ps1
#   .\run.ps1 -json out.json
#   .\run.ps1 -http-source=false
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Args
)

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "go not found on PATH"
    exit 1
}

Write-Host "==> go version"
go version

Write-Host "`n==> fetching golang.org/x/tools"
go get golang.org/x/tools/go/packages@latest
go get golang.org/x/tools/go/ssa@latest
go mod tidy

Write-Host "`n==> go vet"
go vet ./...
if ($LASTEXITCODE -ne 0) { Write-Host "(vet reported issues; continuing)" }

Write-Host "`n==> go test"
go test ./... -v

Write-Host "`n==> go run ."
go run . @Args

Write-Host "`n==> benchmarks"
go test ./... -run XXX -bench . -benchmem -benchtime=300ms

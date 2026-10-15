$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

Write-Host "== removing the dead hand-written grammar ==" -ForegroundColor Cyan
Remove-Item -Force -ErrorAction SilentlyContinue grammar.ebnf

Write-Host "== go mod tidy ==" -ForegroundColor Cyan
go mod tidy

Write-Host "== go test ./... -v ==" -ForegroundColor Cyan
go test ./... -v

Write-Host "== go run . ==" -ForegroundColor Cyan
go run .

Write-Host "== benchmarks ==" -ForegroundColor Cyan
go test ./... -run XXX -bench . -benchmem -benchtime=300ms

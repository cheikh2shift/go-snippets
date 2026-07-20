# run_experiment.ps1
# Go fix modernizers demo — Windows (PowerShell)
# Usage:  .\run_experiment.ps1
# Requires: Go 1.25+ installed and on PATH

$ErrorActionPreference = "Stop"

Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  go fix modernizers — THE EXPERIMENT" -ForegroundColor Cyan
Write-Host "`n========================================" -ForegroundColor Cyan

# 1. Show legacy code BEFORE
Write-Host "`n[1] legacy.go BEFORE go fix:" -ForegroundColor Yellow
Write-Host "----------------------------------------"
Get-Content legacy\legacy.go

# 2. Show the diff go fix WOULD apply (no changes yet)
Write-Host "`n[2] go fix -diff (what the bot wants to change):" -ForegroundColor Yellow
Write-Host "----------------------------------------"
go fix -diff ./...

# 3. Apply the modernizers for real
Write-Host "`n[3] applying go fix ..." -ForegroundColor Yellow
go fix ./...

# 4. Show legacy code AFTER
Write-Host "`n[4] legacy.go AFTER go fix:" -ForegroundColor Green
Write-Host "----------------------------------------"
Get-Content legacy\legacy.go

# 5. Prove behavior is unchanged
Write-Host "`n[5] build + run (behavior must be identical):" -ForegroundColor Yellow
Write-Host "----------------------------------------"
go build -o app.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "BUILD FAILED" -ForegroundColor Red
    exit 1
}
.\app.exe

Write-Host "`n[done] The bot rewrote your code. You wrote none of it." -ForegroundColor Cyan

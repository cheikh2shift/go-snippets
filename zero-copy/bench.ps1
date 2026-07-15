# zerocopy-bench.ps1
# Windows benchmark runner for zerocopy-bench.go
# Requires: Go installed, and wrk for Windows (https://github.com/wg/wrk/releases)
#   OR use the pure-PowerShell fallback below if wrk is not installed.
#
# Usage:
#   1. Start the server in one terminal:   go run zerocopy-bench.go
#   2. Run this script in another:         powershell -ExecutionPolicy Bypass -File bench.ps1
#
# It hits both /raw (zero-copy) and /wrapped (copy-path bug) routes,
# prints req/s + latency, and shows how to capture CPU% and confirm zero-copy.

$ErrorActionPreference = "Stop"

$ServerURL = "http://127.0.0.1:8080"
$Wrk = "wrk"          # expects wrk.exe on PATH; change to full path if needed
$Duration = "30s"
$Threads  = 4
$Conns    = 200

function Run-Wrk($path, $label) {
    Write-Host "`n=== $label ($path) ===" -ForegroundColor Cyan
    & $Wrk -t$Threads -c$Conns -d$Duration "$ServerURL$path"
    if ($LASTEXITCODE -ne 0) {
        Write-Host "wrk failed or not found. Falling back to PowerShell Invoke-WebRequest throughput test..." -ForegroundColor Yellow
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        $n = 0
        $secs = [int]($Duration -replace 's','')
        while ($sw.Elapsed.TotalSeconds -lt $secs) {
            try { Invoke-WebRequest -Uri "$ServerURL$path" -Method GET -UseBasicParsing | Out-Null; $n++ }
            catch { Start-Sleep -Milliseconds 10 }
        }
        $sw.Stop()
        $rps = [math]::Round($n / $sw.Elapsed.TotalSeconds, 1)
        Write-Host "Fallback throughput: $rps req/s over $($sw.Elapsed.TotalSeconds)s ($n requests)" -ForegroundColor Green
    }
}

# 1) Throughput + latency for both routes
Run-Wrk "/raw"     "RAW (zero-copy path)"
Run-Wrk "/wrapped" "WRAPPED (copy-path bug)"

# 2) CPU% capture instructions (Windows)
Write-Host "`n=== CPU capture (run in a 3rd terminal during the test) ===" -ForegroundColor Cyan
Write-Host "  Option A (PowerShell):  Get-Counter '\Process(go)\% Processor Time'"
Write-Host "  Option B (Task Manager): open Details tab, watch go.exe CPU%"
Write-Host "  Option C (typeperf):    typeperf '\Process(go)\% Processor Time' -si 1"

# 3) Confirm zero-copy via strace equivalent on Windows
Write-Host "`n=== Confirm zero-copy (Windows) ===" -ForegroundColor Cyan
Write-Host "  Windows has no strace. Use one of:"
Write-Host "   - Process Monitor (procmon.exe) from Sysinternals: filter on go.exe,"
Write-Host "     watch for ReadFile/WriteFile pairs (copy path) vs. TransmitFile (zero-copy)."
Write-Host "   - Wireshark on loopback: zero-copy still shows packets, but procmon is clearer."
Write-Host "   - Or run the server under WSL2 and use:  strace -e sendfile,splice,read,write -f go run zerocopy-bench.go"
Write-Host "     RAW should show sendfile/splice; WRAPPED shows read/write pairs."

Write-Host "`nDone. Send back req/s, CPU%, and p99 for BOTH /raw and /wrapped." -ForegroundColor Green

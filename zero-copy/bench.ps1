$ErrorActionPreference = "Stop"

$ServerURL = "http://127.0.0.1:8080"
$Duration = 15 # Seconds
$Threads = 4   # Concurrent "clients"

function Run-Benchmark($path, $label) {
    Write-Host "`n=== Benchmarking $label ($path) ===" -ForegroundColor Cyan
    Write-Host "Running with $Threads concurrent jobs for $Duration seconds..." -ForegroundColor Yellow

    $scriptBlock = {
        param($url, $dur)
        # Using HttpClient for minimal overhead
        $client = [System.Net.Http.HttpClient]::new()
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        $n = 0
        
        while ($sw.Elapsed.TotalSeconds -lt $dur) {
            try { 
                # Request headers only to minimize IO overhead
                $res = $client.GetAsync($url).Result
                if ($res.IsSuccessStatusCode) {
                    $n++
                }
            } catch { 
                Start-Sleep -Milliseconds 10 
            }
        }
        return $n
    }

    $jobs = 1..$Threads | ForEach-Object {
        Start-Job -ScriptBlock $scriptBlock -ArgumentList "$ServerURL$path", $Duration
    }

    $results = Receive-Job -Job $jobs -Wait -AutoRemoveJob
    $totalRequests = ($results | Measure-Object -Sum).Sum
    $rps = [math]::Round($totalRequests / $Duration, 1)

    Write-Host "Total requests: $totalRequests" -ForegroundColor Green
    Write-Host "Throughput: $rps req/s" -ForegroundColor Green
}

# Run tests
Run-Benchmark "/raw"     "RAW (zero-copy)"
Run-Benchmark "/wrapped" "WRAPPED (copy-path)"

Write-Host "`nDone." -ForegroundColor Green
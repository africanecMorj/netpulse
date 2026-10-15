$ErrorActionPreference = "Stop"

Write-Host "Starting CPU stress test for 15 seconds..." -ForegroundColor Cyan

docker compose exec testserver sh -lc `
    'cd /tmp && stress-ng --cpu 2 --timeout 15s --metrics-brief'

if ($LASTEXITCODE -ne 0) {
    Write-Host "Stress test failed. Check Docker and stress-ng." -ForegroundColor Red
    exit $LASTEXITCODE
}

Write-Host "Stress test finished." -ForegroundColor Green

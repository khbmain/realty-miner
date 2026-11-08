$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$logDirectory = Join-Path $projectRoot 'logs'
New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
$arguments = '-addr 0.0.0.0:8080 -data "' + (Join-Path $projectRoot 'data') + '" -logs "' + $logDirectory + '"'
while ($true) {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
    $process = Start-Process -FilePath (Join-Path $projectRoot 'bin\dashboard-print.exe') -ArgumentList $arguments -WorkingDirectory $projectRoot -WindowStyle Hidden -Wait -PassThru -RedirectStandardOutput (Join-Path $logDirectory "dashboard-$stamp.out.log") -RedirectStandardError (Join-Path $logDirectory "dashboard-$stamp.err.log")
    Add-Content -LiteralPath (Join-Path $logDirectory 'dashboard-supervisor.log') -Value "$(Get-Date -Format o) exit=$($process.ExitCode); restarting in 3 seconds"
    Start-Sleep -Seconds 3
}



































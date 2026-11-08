$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$logDirectory = Join-Path $projectRoot 'logs'
New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
$logPath = Join-Path $logDirectory ('crawler-' + (Get-Date -Format 'yyyy-MM-dd') + '.log')
$executable = Join-Path $projectRoot 'bin\crawler-page-reports.exe'
$dataDirectory = Join-Path $projectRoot 'data'
if (Test-Path -LiteralPath (Join-Path $dataDirectory 'mining-stopped')) { exit 0 }
Set-Location -LiteralPath $projectRoot
$ErrorActionPreference = 'Continue'
& $executable -pages 99 -delay 5s -out $dataDirectory 2>&1 | ForEach-Object {
    Add-Content -LiteralPath $logPath -Value "$_" -Encoding UTF8
}
exit $LASTEXITCODE




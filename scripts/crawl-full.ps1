$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$logDirectory = Join-Path $projectRoot 'logs'
New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
$logPath = Join-Path $logDirectory ('crawler-' + (Get-Date -Format 'yyyy-MM-dd') + '.log')
$ErrorActionPreference = 'Continue'
& (Join-Path $projectRoot 'bin\crawler.exe') -full -delay 5s -out (Join-Path $projectRoot 'data') 2>&1 | ForEach-Object {
    Add-Content -LiteralPath $logPath -Value "$_" -Encoding UTF8
}
exit $LASTEXITCODE

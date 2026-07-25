$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$cacheRoot = Join-Path $projectRoot "cache"
$env:NODE_PATH = Join-Path $projectRoot "node_modules"
$env:ELECTRON_CACHE = Join-Path $cacheRoot "electron"
$env:ELECTRON_BUILDER_CACHE = Join-Path $cacheRoot "electron-builder"
$env:npm_config_cache = Join-Path $cacheRoot "npm"

$pnpm = "D:\Program Files\pnpm-10.26.2\node_modules\.bin\pnpm.cmd"

Push-Location $projectRoot
try {
    & $pnpm build
    if ($LASTEXITCODE -ne 0) { throw "Production build failed" }

    & (Join-Path $projectRoot "node_modules\.bin\electron-builder.cmd") --config electron-builder.yml --win portable --x64
    if ($LASTEXITCODE -ne 0) { throw "Windows package failed" }
}
finally {
    Pop-Location
}

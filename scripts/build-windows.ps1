[CmdletBinding()]
param(
    [string]$OutputDir = "dist\saki-windows-amd64",
    [switch]$SkipTests,
    [switch]$SkipSMTC,
    [string]$MPVPath
)

$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

if (-not $env:GOPROXY) {
    $env:GOPROXY = 'https://goproxy.cn,direct'
}

function Get-GitOutput {
    param([string[]]$Arguments)

    $output = & git @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ''
    }
    return (($output | Out-String).Trim())
}

function Format-VersionPart {
    param([string]$Value)

    if ($null -eq $Value) {
        $Value = ''
    }
    $part = $Value.Trim() -replace '[^A-Za-z0-9._-]+', '-'
    $part = $part.Trim('-')
    if (-not $part) {
        return 'unknown'
    }
    return $part
}

$baseVersion = if ($env:SAKI_BASE_VERSION) { $env:SAKI_BASE_VERSION } elseif ($env:BASE_VERSION) { $env:BASE_VERSION } else { 'v0.0.1' }
$branchSource = if ($env:SAKI_VERSION_BRANCH) { $env:SAKI_VERSION_BRANCH } else { Get-GitOutput @('rev-parse', '--abbrev-ref', 'HEAD') }
$commitSource = if ($env:SAKI_VERSION_COMMIT) { $env:SAKI_VERSION_COMMIT } else { Get-GitOutput @('rev-parse', '--short=12', 'HEAD') }
$branchName = Format-VersionPart $branchSource
$commitHash = Format-VersionPart $commitSource
if ($env:SAKI_VERSION_DIRTY) {
    $dirty = if ($env:SAKI_VERSION_DIRTY -in @('1', 'true', 'True', 'TRUE', 'dirty')) { 'true' } else { 'false' }
} else {
    $dirtyOutput = Get-GitOutput @('status', '--porcelain')
    $dirty = if ($dirtyOutput) { 'true' } else { 'false' }
}
$versionLdFlags = @(
    '-s',
    '-w',
    "-X github.com/xiongnemo/saki/internal/version.BaseVersion=$baseVersion",
    "-X github.com/xiongnemo/saki/internal/version.BranchName=$branchName",
    "-X github.com/xiongnemo/saki/internal/version.CommitHash=$commitHash",
    "-X github.com/xiongnemo/saki/internal/version.Dirty=$dirty"
) -join ' '

$cgoEnabled = (& go env CGO_ENABLED).Trim()
if ($cgoEnabled -ne "1") {
    throw "CGO_ENABLED must be 1 because the default miniaudio backend uses cgo."
}
$cc = (& go env CC).Trim()
if (-not (Get-Command $cc -ErrorAction SilentlyContinue)) {
    throw "C compiler '$cc' was not found. Install MinGW-w64/GCC or set CC to a working C compiler before building."
}
$targetGoArch = (& go env GOARCH).Trim()
$smtcArch = switch ($targetGoArch) {
    'amd64' { 'x64' }
    '386' { 'x86' }
    'arm64' { 'arm64' }
    default { throw "Unsupported Windows GOARCH for SMTC shim: $targetGoArch" }
}

if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    $outDir = $OutputDir
} else {
    $outDir = Join-Path $repoRoot $OutputDir
}
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$sideBySideShim = Join-Path $outDir 'saki_smtc.dll'
if (Test-Path -LiteralPath $sideBySideShim) {
    try {
        Remove-Item -LiteralPath $sideBySideShim -Force
        Write-Host "Removed stale side-by-side SMTC shim from output directory."
    } catch {
        Write-Warning "Could not remove stale side-by-side SMTC shim from output directory: $($_.Exception.Message)"
    }
}

$isWindowsHost = $env:OS -eq 'Windows_NT'
$shimDir = Join-Path $repoRoot 'internal\mediaintegration\smtc_shim\windows'
$shimDll = Join-Path $shimDir 'saki_smtc.dll'
$shimArchMarker = Join-Path $shimDir 'saki_smtc.dll.arch'
$existingShimArch = ''
if (Test-Path -LiteralPath $shimArchMarker) {
    $existingShimArch = (Get-Content -LiteralPath $shimArchMarker -Raw).Trim()
}
$existingShimMatches = (Test-Path -LiteralPath $shimDll) -and (
    $existingShimArch -eq $smtcArch -or
    (-not $existingShimArch -and $smtcArch -eq 'x64')
)
if ($SkipSMTC) {
    Write-Host "Skipping SMTC shim because -SkipSMTC was specified."
} elseif (-not $isWindowsHost) {
    Write-Host "Skipping SMTC shim: this is not a Windows host."
} else {
    if ($existingShimMatches) {
        Write-Host "Skipping SMTC shim build because an existing $smtcArch DLL was found: $shimDll"
    } else {
        Push-Location $shimDir
        try {
            & powershell -ExecutionPolicy Bypass -File .\build.ps1 -Arch $smtcArch
            if ($LASTEXITCODE -ne 0) {
                throw "SMTC shim build failed with exit code $LASTEXITCODE."
            }
        } finally {
            Pop-Location
        }
    }
}

if (-not (Test-Path -LiteralPath $shimDll)) {
    throw "SMTC shim DLL was not found at $shimDll. The Windows executable embeds this file, so build it first or omit -SkipSMTC."
}
if (Test-Path -LiteralPath $shimArchMarker) {
    $existingShimArch = (Get-Content -LiteralPath $shimArchMarker -Raw).Trim()
}
if ($existingShimArch -and $existingShimArch -ne $smtcArch) {
    throw "SMTC shim architecture mismatch: found $existingShimArch but GOARCH=$targetGoArch requires $smtcArch."
}
if (-not $existingShimArch -and $smtcArch -ne 'x64') {
    throw "SMTC shim architecture is unknown. Rebuild it for GOARCH=$targetGoArch or omit -SkipSMTC."
}
Write-Host "SMTC shim will be embedded from: $shimDll"

if (-not $SkipTests) {
    & go test ./...
    if ($LASTEXITCODE -ne 0) {
        throw "go test failed with exit code $LASTEXITCODE."
    }
}

$exePath = Join-Path $outDir 'saki.exe'
& go build -trimpath -ldflags $versionLdFlags -o $exePath .\cmd\saki
if ($LASTEXITCODE -ne 0) {
    throw "go build failed with exit code $LASTEXITCODE."
}

if ($MPVPath) {
    if (-not (Test-Path -LiteralPath $MPVPath)) {
        throw "MPVPath does not exist: $MPVPath"
    }
    Copy-Item -LiteralPath $MPVPath -Destination (Join-Path $outDir (Split-Path -Leaf $MPVPath)) -Force
    Write-Host "Copied mpv executable into output directory."
} else {
    $mpv = Get-Command mpv.exe -ErrorAction SilentlyContinue
    if ($mpv) {
        Write-Host "Optional mpv found on PATH: $($mpv.Source)"
    } else {
        Write-Host "Optional mpv.exe was not found on PATH. Default miniaudio playback still works for MP3/WAV/FLAC; install mpv for the mpv backend or ALAC/M4A fallback."
    }
}

Write-Host "Build output: $outDir"
Write-Host "Run: $exePath"

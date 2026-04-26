[CmdletBinding()]
param(
    [ValidateSet('x64', 'x86', 'arm64')]
    [string]$Arch = 'x64'
)

$ErrorActionPreference = 'Stop'

$candidateRoots = @()
if ($env:VS_ROOT) {
    $candidateRoots += $env:VS_ROOT
}
if ($env:VS2026_ROOT) {
    $candidateRoots += $env:VS2026_ROOT
}
if ($env:VS2022_ROOT) {
    $candidateRoots += $env:VS2022_ROOT
}
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
if (Test-Path -LiteralPath $vswhere) {
    $vswhereRoots = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    foreach ($root in $vswhereRoots) {
        if ($root) {
            $candidateRoots += $root
        }
    }
}
$candidateRoots += @(
    (Join-Path $env:USERPROFILE 'scoop\apps\vsbuildtools2022\current\vs'),
    (Join-Path $env:USERPROFILE 'scoop\apps\vsbuildtools\current\vs'),
    (Join-Path $env:ProgramFiles 'Microsoft Visual Studio\2022\Enterprise'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\2022\Enterprise'),
    (Join-Path $env:ProgramFiles 'Microsoft Visual Studio\2022\Professional'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\2022\Professional'),
    (Join-Path $env:ProgramFiles 'Microsoft Visual Studio\2022\Community'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\2022\Community'),
    (Join-Path $env:ProgramFiles 'Microsoft Visual Studio\18\BuildTools'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\18\BuildTools'),
    (Join-Path $env:ProgramFiles 'Microsoft Visual Studio\2022\BuildTools'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\2022\BuildTools')
)

$vcvars = $null
foreach ($root in $candidateRoots) {
    if (-not $root) { continue }
    $candidate = Join-Path $root 'VC\Auxiliary\Build\vcvarsall.bat'
    if (Test-Path -LiteralPath $candidate) {
        $vcvars = $candidate
        break
    }
}

if (-not $vcvars) {
    throw "vcvarsall.bat was not found. Install Visual Studio Build Tools with Microsoft.VisualStudio.Workload.VCTools, or set VS_ROOT/VS2026_ROOT/VS2022_ROOT to the VS install root."
}

$probe = @'
where cl >nul
if errorlevel 1 exit /b 10
where link >nul
if errorlevel 1 exit /b 11
where /r "%WindowsSdkDir%Lib" windowsapp.lib >nul
if errorlevel 1 exit /b 12
where /r "%WindowsSdkDir%Include" Windows.Media.Playback.h >nul
if errorlevel 1 exit /b 13
exit /b 0
'@

$probeFile = New-TemporaryFile
Set-Content -LiteralPath $probeFile -Value $probe -Encoding ASCII
try {
    & cmd.exe /d /s /c "call `"$vcvars`" $Arch && call `"$probeFile`""
    if ($LASTEXITCODE -eq 10) { throw "cl.exe was not found after vcvarsall.bat $Arch." }
    if ($LASTEXITCODE -eq 11) { throw "link.exe was not found after vcvarsall.bat $Arch." }
    if ($LASTEXITCODE -eq 12) { throw "windowsapp.lib was not found. Install a Windows SDK component, for example Microsoft.VisualStudio.Component.Windows11SDK.26100." }
    if ($LASTEXITCODE -eq 13) { throw "Windows.Media.Playback.h was not found. Install a Windows SDK component, for example Microsoft.VisualStudio.Component.Windows11SDK.26100." }
    if ($LASTEXITCODE -ne 0) { throw "Visual Studio environment probe failed with exit code $LASTEXITCODE." }
}
finally {
    Remove-Item -LiteralPath $probeFile -ErrorAction SilentlyContinue
}

& cmd.exe /d /s /c "call `"$vcvars`" $Arch && cl /std:c++17 /EHsc /DUNICODE /D_UNICODE /LD saki_smtc.cpp /link windowsapp.lib /OUT:saki_smtc.dll"
if ($LASTEXITCODE -ne 0) {
    throw "SMTC shim build failed with exit code $LASTEXITCODE."
}

Set-Content -LiteralPath 'saki_smtc.dll.arch' -Value $Arch -Encoding ASCII

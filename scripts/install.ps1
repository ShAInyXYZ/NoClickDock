<#
.SYNOPSIS
  NoClickDock 2 installer for Windows.

.DESCRIPTION
  Installs ncd.exe and its core, ncd-core.exe, into
  %LOCALAPPDATA%\NoClickDock\bin, starts NoClickDock at login, and starts it
  now. Settings live in %APPDATA%\NoClickDock\config.json.

  The Python widgets of NoClickDock 1 are stopped first, and their Startup
  shortcuts are moved aside (never deleted) into
  %LOCALAPPDATA%\NoClickDock\legacy-startup: while they run they delete
  every *.json in %APPDATA%\NoClickDock, config.json included.
  -RestoreLegacy puts those shortcuts back.

  The binaries are taken from -From, else from this folder (dist\windows
  holds all three), else from a cross-build in the checkout
  (app\src-tauri\target\x86_64-pc-windows-msvc\release).

  Needs the WebView2 runtime (part of Windows 11 and of current Windows 10).

.EXAMPLE
  .\install.ps1                 install or upgrade, then start
  .\install.ps1 -NoStart        install or upgrade only
  .\install.ps1 -Status         what is installed, nothing changed
  .\install.ps1 -Uninstall      stop, remove the binaries and the login item
                                (settings are kept)
  .\install.ps1 -RestoreLegacy  give the NoClickDock 1 shortcuts back
#>
[CmdletBinding()]
param(
    [string]$From,
    [switch]$NoStart,
    [switch]$Status,
    [switch]$Uninstall,
    [switch]$RestoreLegacy
)

$ErrorActionPreference = 'Stop'
$Here     = Split-Path -Parent $MyInvocation.MyCommand.Path
$Root     = Join-Path $env:LOCALAPPDATA 'NoClickDock'
$Bin      = Join-Path $Root 'bin'
$Aside    = Join-Path $Root 'legacy-startup'
$Startup  = [Environment]::GetFolderPath('Startup')
$RunKey   = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$RunName  = 'NoClickDock'   # the name the app's own launch-at-login uses
$Exe      = Join-Path $Bin 'ncd.exe'

function Say($text) { Write-Host "  $text" }

# -- what is there --------------------------------------------------------
function Get-FileVersionText($path) {
    if (-not (Test-Path $path)) { return '-' }
    $v = (Get-Item $path).VersionInfo.ProductVersion
    if ($v) { return $v } else { return 'present' }
}

function Get-LegacyProcesses {
    Get-CimInstance Win32_Process -Filter "Name like 'python%'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match 'status-checker\.py|ncd-dock\.py|power-monitor\.py' }
}

function Get-LegacyShortcuts {
    @(Get-ChildItem $Startup -Filter 'NCD *.lnk' -ErrorAction SilentlyContinue)
}

function Show-Status {
    $run = (Get-ItemProperty -Path $RunKey -Name $RunName -ErrorAction SilentlyContinue).$RunName
    $rows = @(
        [pscustomobject]@{ What = 'NoClickDock 2';          State = (Get-FileVersionText $Exe) },
        [pscustomobject]@{ What = 'starts at login';        State = $(if ($run) { 'yes' } else { 'no' }) },
        [pscustomobject]@{ What = 'running';                State = $(if (Get-Process ncd -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }) },
        [pscustomobject]@{ What = 'NoClickDock 1 widgets';  State = "$(@(Get-LegacyProcesses).Count) running, $((Get-LegacyShortcuts).Count) at login" },
        [pscustomobject]@{ What = 'WebView2 runtime';       State = $(if (Test-WebView2) { 'yes' } else { 'missing' }) }
    )
    $rows | Format-Table -AutoSize | Out-Host
}

function Test-WebView2 {
    $id = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    foreach ($key in @(
        "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$id",
        "HKLM:\SOFTWARE\Microsoft\EdgeUpdate\Clients\$id",
        "HKCU:\Software\Microsoft\EdgeUpdate\Clients\$id")) {
        $pv = (Get-ItemProperty -Path $key -Name pv -ErrorAction SilentlyContinue).pv
        if ($pv -and $pv -ne '0.0.0.0') { return $true }
    }
    return $false
}

# -- NoClickDock 1 --------------------------------------------------------
function Stop-Legacy {
    $procs = @(Get-LegacyProcesses)
    foreach ($p in $procs) {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
    }
    if ($procs.Count) { Say "stopped $($procs.Count) NoClickDock 1 widget(s)" }
    $links = Get-LegacyShortcuts
    if ($links.Count) {
        New-Item -ItemType Directory -Force -Path $Aside | Out-Null
        foreach ($l in $links) { Move-Item -LiteralPath $l.FullName -Destination $Aside -Force }
        Say "moved $($links.Count) NoClickDock 1 Startup shortcut(s) to $Aside"
    }
}

function Restore-Legacy {
    $links = @(Get-ChildItem $Aside -Filter '*.lnk' -ErrorAction SilentlyContinue)
    foreach ($l in $links) { Move-Item -LiteralPath $l.FullName -Destination $Startup -Force }
    Say "restored $($links.Count) NoClickDock 1 Startup shortcut(s)"
}

# -- NoClickDock 2 --------------------------------------------------------
function Find-Source {
    $candidates = @()
    if ($From) { $candidates += $From }
    $candidates += $Here
    $candidates += (Join-Path $Here '..\app\src-tauri\target\x86_64-pc-windows-msvc\release')
    foreach ($dir in $candidates) {
        if ((Test-Path (Join-Path $dir 'ncd.exe')) -and (Test-Path (Join-Path $dir 'ncd-core.exe'))) { return $dir }
    }
    throw "ncd.exe and ncd-core.exe not found (looked in: $($candidates -join '; ')). Build them or pass -From <folder>."
}

function Stop-Dock {
    $running = @(Get-Process ncd, ncd-core -ErrorAction SilentlyContinue)
    foreach ($p in $running) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue }
    if ($running.Count) { Start-Sleep -Milliseconds 500 }
}

function Install-Dock {
    if (-not (Test-WebView2)) {
        throw 'The Microsoft Edge WebView2 runtime is missing. Install it from https://developer.microsoft.com/microsoft-edge/webview2/ (Evergreen bootstrapper), then run this again.'
    }
    $src = Find-Source
    Stop-Legacy
    Stop-Dock
    New-Item -ItemType Directory -Force -Path $Bin | Out-Null
    foreach ($name in @('ncd.exe', 'ncd-core.exe')) {
        $dst = Join-Path $Bin $name
        Copy-Item -LiteralPath (Join-Path $src $name) -Destination $dst -Force
        Unblock-File -LiteralPath $dst   # downloaded files carry Mark-of-the-Web
    }
    Say "installed into $Bin"
    New-ItemProperty -Path $RunKey -Name $RunName -Value "`"$Exe`"" -PropertyType String -Force | Out-Null
    Say 'starts at login'
    if (-not $NoStart) {
        Start-Process -FilePath $Exe -WorkingDirectory $Bin
        Say 'started'
    }
}

function Uninstall-Dock {
    Stop-Dock
    Remove-ItemProperty -Path $RunKey -Name $RunName -ErrorAction SilentlyContinue
    if (Test-Path $Bin) { Remove-Item -LiteralPath $Bin -Recurse -Force }
    Say "removed $Bin and the login item; settings kept in $(Join-Path $env:APPDATA 'NoClickDock')"
    if ((Get-ChildItem $Aside -Filter '*.lnk' -ErrorAction SilentlyContinue).Count) {
        Say 'NoClickDock 1 shortcuts are still aside: .\install.ps1 -RestoreLegacy puts them back'
    }
}

# -- main -----------------------------------------------------------------
Write-Host ''
Write-Host 'NoClickDock 2'
if ($Status)        { Show-Status; exit 0 }
if ($RestoreLegacy) { Restore-Legacy; Show-Status; exit 0 }
if ($Uninstall)     { Uninstall-Dock; Show-Status; exit 0 }
Install-Dock
Show-Status

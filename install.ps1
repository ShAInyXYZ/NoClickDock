<#
.SYNOPSIS
  NoClickDock installer for Windows.

.DESCRIPTION
  Shows what is installed, at which version, and what this checkout offers;
  you pick what to install, upgrade or remove. Nothing happens to a widget you
  did not select. "Installed" means a copy under %LOCALAPPDATA%\NoClickDock
  with a Startup shortcut pointing at it, so a `git pull` here never changes
  what runs until you choose to upgrade.

  Some widgets read Linux-only interfaces and are marked "not on Windows";
  they are listed for completeness and cannot be selected.

.EXAMPLE
  .\install.ps1                      interactive
  .\install.ps1 -List                table only
  .\install.ps1 -All                 install / upgrade everything supported
  .\install.ps1 -Upgrade             upgrade what is installed and behind
  .\install.ps1 -Install docker,claude
  .\install.ps1 -Remove comfyui
  add -Start to (re)start what was installed, -NoStart to never start
#>
[CmdletBinding()]
param(
    [switch]$List,
    [switch]$All,
    [switch]$Upgrade,
    [string[]]$Install,
    [string[]]$Remove,
    [switch]$Start,
    [switch]$NoStart
)

$ErrorActionPreference = 'Stop'
$Here   = Split-Path -Parent $MyInvocation.MyCommand.Path
$Prefix = if ($env:NCD_HOME) { $env:NCD_HOME } else { Join-Path $env:LOCALAPPDATA 'NoClickDock' }
$Startup = [Environment]::GetFolderPath('Startup')

# key | label | script relative to the checkout | shortcut name | windows?
$Entries = @(
    @{ Key='dock';      Label='Dock';      Rel='dock\ncd-dock.py';                              Id='NCD Dock';      Win=$true  },
    @{ Key='claude';    Label='Claude';    Rel='widgets\claude\claude-status-checker.py';       Id='NCD Claude';    Win=$true  },
    @{ Key='codex';     Label='Codex';     Rel='widgets\codex\codex-status-checker.py';         Id='NCD Codex';     Win=$true  },
    @{ Key='comfyui';   Label='ComfyUI';   Rel='widgets\comfyui\comfyui-status-checker.py';     Id='NCD ComfyUI';   Win=$true  },
    @{ Key='disk';      Label='Disk';      Rel='widgets\disk\disk-status-checker.py';           Id='NCD Disk';      Win=$true  },
    @{ Key='docker';    Label='Docker';    Rel='widgets\docker\docker-status-checker.py';       Id='NCD Docker';    Win=$true  },
    @{ Key='power';     Label='Power';     Rel='widgets\power\power-monitor.py';                Id='NCD Power';     Win=$false },
    @{ Key='tailscale'; Label='Tailscale'; Rel='widgets\tailscale\tailscale-status-checker.py'; Id='NCD Tailscale'; Win=$true  }
)

# -- python ---------------------------------------------------------------
# GTK3 on Windows comes from MSYS2, so the interpreter that can import gi is
# usually not the one on PATH. Prefer an explicit PYTHON, then the MSYS2
# UCRT64 python, then whatever `python` resolves to.
function Find-Python {
    $candidates = @()
    if ($env:PYTHON) { $candidates += $env:PYTHON }
    $candidates += @(
        'C:\msys64\ucrt64\bin\python3.exe',
        'C:\msys64\mingw64\bin\python3.exe'
    )
    foreach ($c in (Get-Command python3, python -ErrorAction SilentlyContinue)) {
        $candidates += $c.Source
    }
    foreach ($p in $candidates) {
        if (-not $p -or -not (Test-Path $p)) { continue }
        & $p -c "import gi; gi.require_version('Gtk','3.0'); from gi.repository import Gtk" 2>$null
        if ($LASTEXITCODE -eq 0) { return $p }
    }
    return $null
}

$Py = Find-Python
if (-not $Py) {
    Write-Host ""
    Write-Host "  No Python with GTK3 bindings was found." -ForegroundColor Red
    Write-Host ""
    Write-Host "  NoClickDock draws its dots and panels with GTK3, which on Windows"
    Write-Host "  comes from MSYS2:"
    Write-Host ""
    Write-Host "    1. Install MSYS2 from https://www.msys2.org/"
    Write-Host "    2. In the MSYS2 UCRT64 terminal:"
    Write-Host "         pacman -S mingw-w64-ucrt-x86_64-python-gobject mingw-w64-ucrt-x86_64-gtk3"
    Write-Host "    3. Run this installer again."
    Write-Host ""
    Write-Host "  If MSYS2 lives somewhere other than C:\msys64, point at it:"
    Write-Host '         $env:PYTHON = "D:\msys64\ucrt64\bin\python3.exe"'
    Write-Host ""
    exit 1
}

# -- scan -----------------------------------------------------------------
function Get-Version($path) {
    if (-not (Test-Path $path)) { return $null }
    foreach ($line in Get-Content -LiteralPath $path -TotalCount 40) {
        if ($line -match '^__version__ = "([^"]+)"') { return $Matches[1] }
    }
    return $null
}

function Get-InstalledScript($shortcutName) {
    $lnk = Join-Path $Startup "$shortcutName.lnk"
    if (-not (Test-Path $lnk)) { return $null }
    try {
        $sh = New-Object -ComObject WScript.Shell
        $args = $sh.CreateShortcut($lnk).Arguments
        # the last quoted or bare token is the script
        if ($args -match '"([^"]+\.py)"') { return $Matches[1] }
        if ($args -match '(\S+\.py)')      { return $Matches[1] }
    } catch { }
    return $null
}

function Test-Running($rel) {
    $leaf = Split-Path -Leaf $rel
    $procs = Get-CimInstance Win32_Process -Filter "Name like 'python%'" -ErrorAction SilentlyContinue
    foreach ($p in $procs) {
        if ($p.CommandLine -and $p.CommandLine -match [regex]::Escape($leaf)) { return $true }
    }
    return $false
}

function Scan {
    foreach ($e in $Entries) {
        $e.Avail = Get-Version (Join-Path $Here $e.Rel)
        if (-not $e.Avail) { $e.Avail = '?' }
        $e.Path  = Get-InstalledScript $e.Id
        $e.Run   = if (Test-Running $e.Rel) { 'running' } else { '' }
        $target  = Join-Path $Prefix $e.Rel
        if (-not $e.Win) {
            $e.Inst = '—'; $e.State = 'not on Windows (needs /proc, lm-sensors)'
        } elseif (-not $e.Path) {
            $e.Inst = '—'; $e.State = 'not installed'
        } elseif (-not (Test-Path $e.Path)) {
            $e.Inst = '?'; $e.State = "broken: $($e.Path) is missing"
        } else {
            $e.Inst = Get-Version $e.Path
            if (-not $e.Inst) { $e.Inst = 'legacy' }
            if ($e.Inst -eq $e.Avail -and $e.Path -eq $target) {
                $e.State = 'up to date'
            } elseif ($e.Path -ne $target) {
                $e.State = "upgrade: runs from $(Split-Path -Parent $e.Path)"
            } else {
                $e.State = "upgrade $($e.Inst) -> $($e.Avail)"
            }
        }
    }
}

function Show-Table {
    Write-Host ""
    Write-Host "  NoClickDock - checkout at $Here"
    Write-Host "  python: $Py"
    Write-Host ""
    Write-Host ("  {0,-3} {1,-11} {2,-10} {3,-10} {4,-9} {5}" -f '#','widget','installed','available','','status')
    for ($i = 0; $i -lt $Entries.Count; $i++) {
        $e = $Entries[$i]
        $mark = if ($e.Sel) { '*' } else { ' ' }
        $line = ("  {0}{1,-2} {2,-11} {3,-10} {4,-10} {5,-9} {6}" -f
                 $mark, ($i + 1), $e.Label, $e.Inst, $e.Avail, $e.Run, $e.State)
        if (-not $e.Win) { Write-Host $line -ForegroundColor DarkGray }
        elseif ($e.State -like 'upgrade*' -or $e.State -like 'broken*') { Write-Host $line -ForegroundColor Yellow }
        else { Write-Host $line }
    }
    Write-Host ""
}

# -- actions --------------------------------------------------------------
function Start-One($e) {
    $script = Join-Path $Prefix $e.Rel
    # pythonw.exe runs without a console window; fall back to python.exe
    $exe = $Py -replace 'python3\.exe$', 'pythonw.exe' -replace 'python\.exe$', 'pythonw.exe'
    if (-not (Test-Path $exe)) { $exe = $Py }
    Start-Process -FilePath $exe -ArgumentList "`"$script`"" `
                  -WorkingDirectory (Split-Path -Parent $script) `
                  -WindowStyle Hidden | Out-Null
}

function Stop-One($e) {
    $leaf = Split-Path -Leaf $e.Rel
    $procs = Get-CimInstance Win32_Process -Filter "Name like 'python%'" -ErrorAction SilentlyContinue
    foreach ($p in $procs) {
        if ($p.CommandLine -and $p.CommandLine -match [regex]::Escape($leaf)) {
            Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
        }
    }
}

function Copy-Tree($src, $dst) {
    if (-not (Test-Path $dst)) { New-Item -ItemType Directory -Path $dst -Force | Out-Null }
    # mirror, minus __pycache__: robocopy is the only reliable /MIR on Windows
    $null = robocopy $src $dst /MIR /NFL /NDL /NJH /NJS /NC /NS /NP /XD __pycache__
    if ($LASTEXITCODE -ge 8) { throw "copy failed: $src -> $dst" }
    $global:LASTEXITCODE = 0
}

function Install-One($e) {
    $srcDir = Join-Path $Here   (Split-Path -Parent $e.Rel)
    $dstDir = Join-Path $Prefix (Split-Path -Parent $e.Rel)
    Copy-Tree $srcDir $dstDir
    # the shared runtime every widget imports
    Copy-Tree (Join-Path $Here 'core') (Join-Path $Prefix 'core')

    $exe = $Py -replace 'python3\.exe$', 'pythonw.exe' -replace 'python\.exe$', 'pythonw.exe'
    if (-not (Test-Path $exe)) { $exe = $Py }
    $script = Join-Path $Prefix $e.Rel
    $sh = New-Object -ComObject WScript.Shell
    $lnk = $sh.CreateShortcut((Join-Path $Startup "$($e.Id).lnk"))
    $lnk.TargetPath       = $exe
    $lnk.Arguments        = "`"$script`""
    $lnk.WorkingDirectory = Split-Path -Parent $script
    $lnk.Description      = "NoClickDock - $($e.Label)"
    $lnk.Save()
    Write-Host "  installed $($e.Label) $($e.Avail) -> $script"
}

function Remove-One($e) {
    Stop-One $e
    $lnk = Join-Path $Startup "$($e.Id).lnk"
    if (Test-Path $lnk) { Remove-Item $lnk -Force }
    if ($e.Key -ne 'dock') {
        $dir = Join-Path $Prefix (Split-Path -Parent $e.Rel)
        if (Test-Path $dir) { Remove-Item $dir -Recurse -Force }
    }
    Write-Host "  removed $($e.Label) (your config under %APPDATA% is kept)"
}

function Invoke-InstallSelected($startMode) {
    # the dock rides along with any widget
    if ($Entries | Where-Object { $_.Sel -and $_.Key -ne 'dock' }) {
        ($Entries | Where-Object Key -eq 'dock').Sel = $true
    }
    foreach ($e in $Entries) {
        if (-not $e.Sel) { continue }
        if (-not $e.Win) { Write-Host "  skipped $($e.Label): not supported on Windows" -ForegroundColor DarkGray; continue }
        $was = $e.Run
        $unchanged = ($e.State -eq 'up to date')
        Install-One $e
        if ($e.Key -eq 'dock' -and $unchanged -and $was) { continue }
        switch ($startMode) {
            'yes' { Stop-One $e; Start-One $e; Write-Host "  started $($e.Label)" }
            'no'  { }
            default { if ($was) { Stop-One $e; Start-One $e; Write-Host "  restarted $($e.Label)" } }
        }
    }
}

function Invoke-RemoveSelected {
    foreach ($e in $Entries) { if ($e.Sel) { Remove-One $e } }
    # a dock with nothing left to hold is not worth keeping; check the Startup
    # folder after the removals, not before
    $dock = $Entries | Where-Object Key -eq 'dock'
    $others = @(Get-ChildItem $Startup -Filter 'NCD *.lnk' -ErrorAction SilentlyContinue |
                Where-Object { $_.Name -ne 'NCD Dock.lnk' })
    if (-not $dock.Sel -and $others.Count -eq 0 -and (Test-Path (Join-Path $Startup 'NCD Dock.lnk'))) {
        Remove-One $dock
    }
}

function Set-Selection($what, $names) {
    foreach ($e in $Entries) {
        switch ($what) {
            'all'      { if ($e.Win) { $e.Sel = $true } }
            'new'      { if ($e.Win -and $e.State -eq 'not installed') { $e.Sel = $true } }
            'upgrades' { if ($e.Win -and ($e.State -like 'upgrade*' -or $e.State -like 'broken*')) { $e.Sel = $true } }
            'names'    { if ($names -contains $e.Key) { $e.Sel = $true } }
        }
    }
}

# -- run ------------------------------------------------------------------
foreach ($e in $Entries) { $e.Sel = $false }
Scan

$startMode = ''
if ($Start)   { $startMode = 'yes' }
if ($NoStart) { $startMode = 'no' }

if ($List -or $All -or $Upgrade -or $Install -or $Remove) {
    if ($All)     { Set-Selection 'all' $null }
    if ($Upgrade) { Set-Selection 'upgrades' $null }
    if ($Install) { Set-Selection 'names' $Install }
    if ($Remove)  { Set-Selection 'names' $Remove }
    Show-Table
    if ($List)               { exit 0 }
    elseif ($Remove)         { Invoke-RemoveSelected }
    else                     { Invoke-InstallSelected $startMode }
    exit 0
}

while ($true) {
    Show-Table
    Write-Host "  toggle: 1-$($Entries.Count)   a=all  n=not installed  u=upgrades  c=clear"
    Write-Host "  then:   i=install/upgrade selected   r=remove selected   q=quit"
    $cmd = Read-Host "  >"
    switch -Regex ($cmd) {
        '^q$' { exit 0 }
        '^a$' { Set-Selection 'all' $null }
        '^n$' { Set-Selection 'new' $null }
        '^u$' { Set-Selection 'upgrades' $null }
        '^c$' { foreach ($e in $Entries) { $e.Sel = $false } }
        '^i$' {
            $mode = $startMode
            if (-not $mode) {
                $yn = Read-Host "  start / restart the selected widgets now? [Y/n]"
                $mode = if ($yn -match '^[Nn]') { 'no' } else { 'yes' }
            }
            Invoke-InstallSelected $mode
            foreach ($e in $Entries) { $e.Sel = $false }
            Scan
        }
        '^r$' { Invoke-RemoveSelected; foreach ($e in $Entries) { $e.Sel = $false }; Scan }
        default {
            foreach ($t in ($cmd -split '\s+')) {
                if ($t -match '^\d+$') {
                    $i = [int]$t - 1
                    if ($i -ge 0 -and $i -lt $Entries.Count) { $Entries[$i].Sel = -not $Entries[$i].Sel }
                }
            }
        }
    }
}

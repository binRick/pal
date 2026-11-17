# pal installer for Windows PowerShell.
#
#   irm https://raw.githubusercontent.com/binRick/pal/main/install.ps1 | iex
#
# Environment:
#   PAL_VERSION       release tag to install, e.g. v0.2.1   (default: latest)
#   PAL_INSTALL_DIR   where to put pal.exe                  (default: %LOCALAPPDATA%\pal)
$ErrorActionPreference = 'Stop'

$repo = 'binRick/pal'
$asset = 'pal-windows-amd64.exe'
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64' -and $env:PROCESSOR_ARCHITECTURE -ne 'ARM64') {
    throw "pal install: unsupported CPU $env:PROCESSOR_ARCHITECTURE. Build from source: https://github.com/$repo"
}

$version = if ($env:PAL_VERSION) { $env:PAL_VERSION } else { 'latest' }
$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" }
        else { "https://github.com/$repo/releases/download/$version" }
$dir = if ($env:PAL_INSTALL_DIR) { $env:PAL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'pal' }

New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir 'pal.exe'
Write-Host "Downloading $asset ($version)..."
Invoke-WebRequest -Uri "$base/$asset" -OutFile $dest -UseBasicParsing

try {
    $sums = (Invoke-WebRequest -Uri "$base/checksums.txt" -UseBasicParsing).Content
    $line = ($sums -split "`n") | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))\s*$" } | Select-Object -First 1
    if ($line) {
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 $dest).Hash.ToLower()
        if ($want -ne $got) { Remove-Item $dest; throw "pal install: checksum mismatch for $asset" }
        Write-Host 'Checksum OK.'
    } else { Write-Host "Checksum not verified (no entry for $asset)." }
} catch [System.Net.WebException] { Write-Host 'Checksum not verified (this release publishes none).' }
catch { if ($_.Exception.Message -like '*checksum mismatch*') { throw } else { Write-Host 'Checksum not verified (this release publishes none).' } }

Write-Host "Installed $dest"
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
    Write-Host "Added $dir to your user PATH. Open a new terminal, then run: pal"
} else { Write-Host 'Run: pal' }

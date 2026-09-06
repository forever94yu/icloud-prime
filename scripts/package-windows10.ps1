param(
  [Parameter(Mandatory = $true)]
  [string]$Version,

  [Parameter(Mandatory = $true)]
  [string]$BinaryPath,

  [string]$OutputRoot = "build\release"
)

$ErrorActionPreference = "Stop"

function Assert-InsideDirectory {
  param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$Parent
  )

  $fullPath = [System.IO.Path]::GetFullPath($Path)
  $fullParent = [System.IO.Path]::GetFullPath($Parent).TrimEnd('\') + '\'
  if (-not $fullPath.StartsWith($fullParent, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to operate outside output root: $fullPath"
  }
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$binaryFullPath = (Resolve-Path $BinaryPath).Path
$outputRootFullPath = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $OutputRoot))
$packageName = "icloud-prime-windows10-portable-v$Version"
$stageDir = Join-Path $outputRootFullPath $packageName
$zipPath = Join-Path $outputRootFullPath "$packageName.zip"

New-Item -ItemType Directory -Path $outputRootFullPath -Force | Out-Null

Assert-InsideDirectory -Path $stageDir -Parent $outputRootFullPath
Assert-InsideDirectory -Path $zipPath -Parent $outputRootFullPath

if (Test-Path -LiteralPath $stageDir) {
  Remove-Item -LiteralPath $stageDir -Recurse -Force
}
if (Test-Path -LiteralPath $zipPath) {
  Remove-Item -LiteralPath $zipPath -Force
}

New-Item -ItemType Directory -Path $stageDir | Out-Null
New-Item -ItemType Directory -Path (Join-Path $stageDir "data") | Out-Null
New-Item -ItemType Directory -Path (Join-Path $stageDir "logs") | Out-Null

Copy-Item -LiteralPath $binaryFullPath -Destination (Join-Path $stageDir "icloud-prime.exe") -Force

Set-Content -LiteralPath (Join-Path $stageDir "start.bat") -Encoding ASCII -Value @"
@echo off
cd /d "%~dp0"
if not exist data mkdir data
if not exist logs mkdir logs
start "iCloud Prime" /min cmd /c "icloud-prime.exe -addr 127.0.0.1:8081 -data .\data > .\logs\server.out.log 2> .\logs\server.err.log"
echo iCloud Prime started.
echo Open http://127.0.0.1:8081 in your browser.
pause
"@

Set-Content -LiteralPath (Join-Path $stageDir "stop.bat") -Encoding ASCII -Value @"
@echo off
taskkill /F /IM icloud-prime.exe
pause
"@

Set-Content -LiteralPath (Join-Path $stageDir "README-Usage.txt") -Encoding ASCII -Value @"
iCloud Prime Windows 10 Portable

How to use:
1. Extract the whole zip to a fixed folder, for example D:\Tools\icloud-prime.
2. Double-click start.bat.
3. Open http://127.0.0.1:8081 in your browser.
4. Add your own iCloud account in the web console.
5. Configure your own Cookie or App-specific password.
6. Create one Hide My Email alias, batch-create aliases, or schedule automatic alias creation.
7. Read mail sent to an alias when needed.
8. Double-click stop.bat when you want to stop the service.

Security notes:
1. This package does not include any real account data.
2. The first run may create data\accounts.json on your computer.
3. data\accounts.json can contain Cookie values, App-specific passwords, and proxy settings.
4. Automatic creation jobs are stored locally in data\create_jobs.json.
5. Do not share or upload data\accounts.json or data\create_jobs.json.
6. logs\ only stores local runtime logs.
7. The portable launcher only accepts local connections. Remote access requires
   ICLOUD_PRIME_API_TOKEN and an explicit -addr :8081 argument.

Example config:
data\accounts.example.json only shows the field format. The recommended path is
adding accounts in the web console. If you manually replace placeholders in
accounts.example.json and data\accounts.json does not exist yet, the app imports
the edited example once and saves it as data\accounts.json.
"@

Set-Content -LiteralPath (Join-Path $stageDir "data\accounts.example.json") -Encoding ASCII -Value @"
{
  "accounts": {
    "acc_1": {
      "id": "acc_1",
      "name": "Example account",
      "host": "icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "PASTE_YOUR_COOKIE_VALUE_HERE",
        "X-APPLE-WEBAUTH-USER": "PASTE_YOUR_COOKIE_VALUE_HERE",
        "X-APPLE-DS-WEB-SESSION-TOKEN": "PASTE_YOUR_COOKIE_VALUE_HERE"
      },
      "icloud_email": "your_email@icloud.com",
      "app_password": "xxxx-xxxx-xxxx-xxxx",
      "status": "pending"
    }
  }
}
"@

$releaseNotes = @(
  "# iCloud Prime v$Version",
  "",
  "Windows 10 portable release.",
  "",
  "## Windows 10 portable package",
  "",
  "Asset:",
  "",
  "- $packageName.zip",
  "",
  "Usage:",
  "",
  "1. Download and extract the zip.",
  "2. Double-click start.bat.",
  "3. Open http://127.0.0.1:8081.",
  "4. Add your own account and configure Cookie or App-specific password.",
  "5. Create aliases, batch-create aliases, schedule automatic jobs, or read mail.",
  "6. Use stop.bat to stop the service.",
  "",
  "## Security",
  "",
  "- The release package contains no real account data.",
  "- The package only includes accounts.example.json with placeholders.",
  "- Real account data is saved locally to data\accounts.json after you run the app.",
  "- If accounts.json is missing, an edited accounts.example.json with real values is imported once.",
  "- Automatic job data is saved locally to data\create_jobs.json after you create jobs.",
  "- Do not share data\accounts.json, data\create_jobs.json, Cookie values, or App-specific passwords."
)
Set-Content -LiteralPath (Join-Path $outputRootFullPath "v$Version-notes.md") -Encoding ASCII -Value $releaseNotes

$changelogPath = Join-Path $repoRoot "CHANGELOG.md"
if (Test-Path -LiteralPath $changelogPath) {
  $changelog = Get-Content -LiteralPath $changelogPath -Raw
  $escapedVersion = [regex]::Escape($Version)
  $versionChanges = [regex]::Match($changelog, "(?ms)^## v$escapedVersion[^\r\n]*\r?\n(.*?)(?=^## |\z)")
  if ($versionChanges.Success) {
    Add-Content -LiteralPath (Join-Path $outputRootFullPath "v$Version-notes.md") -Encoding UTF8 -Value ("`n## Changes`n" + $versionChanges.Groups[1].Value.Trim())
  }
}

Compress-Archive -Path (Join-Path $stageDir "*") -DestinationPath $zipPath -Force

[pscustomobject]@{
  PackageDirectory = $stageDir
  ZipPath = $zipPath
}

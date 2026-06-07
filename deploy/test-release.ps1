param(
  [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

if ($Version -eq "") {
  $appGo = Get-Content "cmd\lwylink-windows-ui\app.go" -Raw
  if ($appGo -notmatch 'var AppVersion = "([^"]+)"') {
    throw "无法从 app.go 读取 AppVersion"
  }
  $Version = $Matches[1]
}

PowerShell -NoProfile -ExecutionPolicy Bypass -File ".\deploy\release.ps1" `
  -Version $Version `
  -SkipBuild `
  -SkipDeploy `
  -SkipClientRelease | Out-Host

$adminHTML = Get-Content "internal\web\admin_assets\admin.html" -Raw
$checks = @{
  '版本号占位符' = 'name="version" placeholder="例如 ' + $Version + '"'
  '流量占位符' = 'placeholder="例如 1024"'
  '安装参数占位符' = 'placeholder="例如 /S，可留空"'
}

foreach ($item in $checks.GetEnumerator()) {
  if (-not $adminHTML.Contains($item.Value)) {
    throw "release 脚本占位符检查失败：$($item.Key)"
  }
}

Write-Host "release 脚本自测通过：$Version" -ForegroundColor Green

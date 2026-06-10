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
  '后台固定强制更新' = '当前阶段所有客户端发布都会强制更新'
}

foreach ($item in $checks.GetEnumerator()) {
  if (-not $adminHTML.Contains($item.Value)) {
    throw "release 脚本占位符检查失败：$($item.Key)"
  }
}

$releaseScript = Get-Content "deploy\release.ps1" -Raw
if (-not $releaseScript.Contains('$content.Add([System.Net.Http.StringContent]::new("true"), "mandatory")')) {
  throw "release 脚本必须默认发布强制更新"
}
if ($releaseScript.Contains('git add -A')) {
  throw "release 脚本提交不能使用 git add -A，避免误提交未跟踪文件"
}
if (-not $releaseScript.Contains('git add -u')) {
  throw "release 脚本提交必须只纳入已跟踪文件"
}
if (-not $releaseScript.Contains('SkipPostReleaseCheck')) {
  throw "release 脚本必须支持跳过发布后巡检"
}
if (-not $releaseScript.Contains('post-release-check.ps1", "-ExpectedVersion", $Version')) {
  throw "release 脚本必须把发布版本传给发布后巡检"
}
$uploadScript = Get-Content "deploy\upload-deploy.ps1" -Raw
if (-not $uploadScript.Contains('ValidateDeployAuth')) {
  throw "upload-deploy 脚本必须提供部署认证预检模式"
}
if (-not $uploadScript.Contains('-o BatchMode=yes')) {
  throw "upload-deploy 脚本必须用 BatchMode 预检 native SSH，避免无凭据时卡住"
}
if (-not $uploadScript.Contains('New-ServiceFileForRemoteRoot') -or -not $uploadScript.Contains('.Replace("/opt/lwylink", $RemoteRoot)')) {
  throw "upload-deploy 脚本必须按 RemoteRoot 生成 systemd 文件"
}
$postReleaseScript = Get-Content "deploy\post-release-check.ps1" -Raw
foreach ($needle in @(
  "SkipCoturn",
  "SkipBackupTimer",
  "ExpectedVersion",
  "ExpectedSHA256",
  "SkipDownload",
  "SkipMetrics",
  "SkipBackupManifest",
  "MaxBackupAgeHours",
  "latest download",
  "backup manifest",
  "runtime-data.tgz",
  "runtime-data.files",
  "Get-FileHash -Path `$tmp -Algorithm SHA256",
  "lwylink_coturn_usage_collector_enabled",
  "lwylink_turn_charge_failures_total",
  "lwylink_downloads_last_day"
)) {
  if (-not $postReleaseScript.Contains($needle)) {
    throw "post-release-check 脚本缺少 $needle 参数或校验"
  }
}

$releaseManageScript = Get-Content "deploy\manage-client-releases.ps1" -Raw
foreach ($needle in @(
  'ValidateSet("list", "clear", "keep-latest")',
  "DryRun",
  "Get-ClientReleases",
  "Remove-ClientRelease",
  "admin/releases/delete",
  "keep-latest",
  "Get-VersionSortKey"
)) {
  if (-not $releaseManageScript.Contains($needle)) {
    throw "manage-client-releases 脚本缺少 $needle"
  }
}

$statusReportScript = Get-Content "deploy\status-report.ps1" -Raw
foreach ($needle in @(
  "LwyLink 线上状态报告",
  "healthz",
  "api/client/releases/latest",
  "Platform",
  "lwylink_rooms_online",
  "lwylink_overlay_connections",
  "lwylink_nodes_total",
  "lwylink_coturn_log_lag_seconds",
  "lwylink-backup.timer",
  "Read-BackupSummary",
  "runtime_data",
  "clientReleases",
  "offset_count",
  "ConvertTo-Json -Depth 6"
)) {
  if (-not $statusReportScript.Contains($needle)) {
    throw "status-report 脚本缺少 $needle"
  }
}

$backupScript = Get-Content "deploy\backup-remote.ps1" -Raw
foreach ($needle in @(
  "SkipRuntimeData",
  "runtime-data.tgz",
  "runtime-data.files",
  "data/client-releases",
  "coturn-usage.offset",
  "__SKIP_RUNTIME_DATA__"
)) {
  if (-not $backupScript.Contains($needle)) {
    throw "backup-remote 脚本缺少 $needle"
  }
}

$clientUpgradeScript = Get-Content "deploy\test-client-upgrade.ps1" -Raw
foreach ($needle in @(
  "BuildOldClient",
  "ApplySelfUpdater",
  "LaunchOldClient",
  "PrepareTestLogin",
  "RestoreSavedLogin",
  "ClickInstallUpdate",
  "Click-LwyLinkInstallButton",
  "UIAutomationClient",
  "Wait-ExecutableSHA256",
  "credentials.before-upgrade-test",
  "Write-JsonNoBom",
  "ProtectedData",
  "api/smoke/register",
  "AppVersion=$OldVersion",
  "api/client/releases/latest",
  "mandatory",
  "Get-FileHash -Path `$downloadPath -Algorithm SHA256",
  "--lwylink-apply-update",
  "LWYLINK_UPGRADE_TEST_STATE"
  "LWYLINK_UPGRADE_TEST_AUTO_INSTALL"
)) {
  if (-not $clientUpgradeScript.Contains($needle)) {
    throw "test-client-upgrade 脚本缺少 $needle"
  }
}

$urlChecks = @(
  @{
    name = "无参数默认值"
    args = @()
    want = "https://www.lwylink.xyz"
  },
  @{
    name = "部署 HostName 不影响公开 API"
    args = @("-HostName", "64.83.34.53")
    want = "https://www.lwylink.xyz"
  },
  @{
    name = "显式 ApiURL"
    args = @("-ApiURL", "http://127.0.0.1:8080")
    want = "http://127.0.0.1:8080"
  },
  @{
    name = "带协议 HostName"
    args = @("-HostName", "https://www.lwylink.xyz")
    want = "https://www.lwylink.xyz"
  },
  @{
    name = "本地部署配置"
    before = {
      $configPath = "configs\deploy.local.json"
      $backupPath = ""
      if (Test-Path $configPath) {
        $backupPath = "configs\deploy.local.json.test-backup"
        Move-Item -LiteralPath $configPath -Destination $backupPath -Force
      }
      [pscustomobject]@{
        configPath = $configPath
        backupPath = $backupPath
      }
    }
    setup = {
      param($state)
      $json = @{
        apiURL = "https://local-config.example"
      } | ConvertTo-Json
      Set-Content -LiteralPath $state.configPath -Value $json -Encoding UTF8
    }
    cleanup = {
      param($state)
      Remove-Item -LiteralPath $state.configPath -Force -ErrorAction SilentlyContinue
      if ($state.backupPath) {
        Move-Item -LiteralPath $state.backupPath -Destination $state.configPath -Force
      }
    }
    args = @()
    want = "https://local-config.example"
  }
)

foreach ($case in $urlChecks) {
  $state = $null
  if ($case.before) {
    $state = & $case.before
  }
  try {
    if ($case.setup) {
      & $case.setup $state
    }
    $actual = PowerShell -NoProfile -ExecutionPolicy Bypass -File ".\deploy\release.ps1" `
      -Version $Version `
      -ValidateReleaseApiURL `
      @($case.args)
    if ($LASTEXITCODE -ne 0) {
      throw "release API URL 校验命令失败：$($case.name)"
    }
    $actual = [string]::Join("", $actual).Trim()
    if ($actual -ne $case.want) {
      throw "release API URL 校验失败：$($case.name)，got $actual, want $($case.want)"
    }
  } finally {
    if ($case.cleanup) {
      & $case.cleanup $state
    }
  }
}

Write-Host "release 脚本自测通过：$Version" -ForegroundColor Green

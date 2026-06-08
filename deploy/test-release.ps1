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
$uploadScript = Get-Content "deploy\upload-deploy.ps1" -Raw
if (-not $uploadScript.Contains('ValidateDeployAuth')) {
  throw "upload-deploy 脚本必须提供部署认证预检模式"
}
if (-not $uploadScript.Contains('-o BatchMode=yes')) {
  throw "upload-deploy 脚本必须用 BatchMode 预检 native SSH，避免无凭据时卡住"
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

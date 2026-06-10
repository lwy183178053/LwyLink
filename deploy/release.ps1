param(
  [Parameter(Mandatory = $true)]
  [string]$Version,
  [string]$Notes = "",
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$RemoteRoot = "",
  [string]$AdminToken = "",
  [string]$ApiURL = "",
  [string]$OverlayAddr = "",
  [switch]$UploadConfigs,
  [switch]$SkipDeploy,
  [switch]$SkipSmoke,
  [switch]$SkipClientRelease,
  [switch]$SkipBuild,
  [switch]$SkipBackup,
  [switch]$SkipPostReleaseCheck,
  [switch]$SkipTests,
  [switch]$NoVet,
  [switch]$NoInstallWails,
  [switch]$Commit,
  [switch]$ValidateReleaseApiURL,
  [switch]$InstallDailyBackup,
  [string]$BackupRoot = "",
  [int]$BackupRetentionDays = 14
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$windowsDistDir = Join-Path $repoRoot "build\windows-amd64"
$clientExe = Join-Path $windowsDistDir "LwyLink.exe"
$artifactManifest = Join-Path $repoRoot "build\artifacts.json"
$utf8NoBOM = [System.Text.UTF8Encoding]::new($false)
$defaultPublicApiURL = "https://www.lwylink.xyz"
$deployLocalConfigPath = Join-Path $repoRoot "configs\deploy.local.json"
$deployLocalConfig = $null
if (Test-Path $deployLocalConfigPath) {
  $deployLocalConfig = Get-Content $deployLocalConfigPath -Raw | ConvertFrom-Json
}

function Invoke-Step {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
}

function Set-TextFileVersion {
  param(
    [string]$Path,
    [string]$Pattern,
    [string]$Replacement,
    [int]$Count = 0
  )
  $fullPath = Join-Path $repoRoot $Path
  $text = Get-Content $fullPath -Raw
  $regex = [regex]::new($Pattern)
  if (-not $regex.IsMatch($text)) {
    throw "版本替换未命中：$Path"
  }
  if ($Count -gt 0) {
    $next = $regex.Replace($text, $Replacement, $Count)
  } else {
    $next = $regex.Replace($text, $Replacement)
  }
  if ($next -eq $text) {
    return
  }
  [System.IO.File]::WriteAllText($fullPath, $next, $utf8NoBOM)
}

function Get-DeployConfigValue {
  param(
    [string]$CliValue,
    [string]$EnvName,
    [string]$ConfigName,
    [string]$DefaultValue = ""
  )
  if (-not [string]::IsNullOrWhiteSpace($CliValue)) { return $CliValue }
  $envValue = [Environment]::GetEnvironmentVariable($EnvName)
  if (-not [string]::IsNullOrWhiteSpace($envValue)) { return $envValue }
  if ($deployLocalConfig -and $deployLocalConfig.PSObject.Properties.Name -contains $ConfigName) {
    $configValue = [string]$deployLocalConfig.$ConfigName
    if (-not [string]::IsNullOrWhiteSpace($configValue)) { return $configValue }
  }
  return $DefaultValue
}

function Resolve-DeploySettings {
  if ($HostName -eq "") { $script:HostName = Get-DeployConfigValue "" "LWYLINK_DEPLOY_HOST" "hostName" "" }
  if ($User -eq "") { $script:User = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_USER" "user" "" }
  if ($Port -eq 0) {
    $portValue = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PORT" "port" ""
    if ($portValue -ne "") { $script:Port = [int]$portValue }
  }
  if ($Password -eq "") { $script:Password = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PASSWORD" "password" "" }
  if ($RemoteRoot -eq "") { $script:RemoteRoot = Get-DeployConfigValue "" "LWYLINK_REMOTE_ROOT" "remoteRoot" "/opt/lwylink" }
  if ($ApiURL -eq "") { $script:ApiURL = Get-DeployConfigValue "" "LWYLINK_API_URL" "apiURL" "" }
  if ($OverlayAddr -eq "") { $script:OverlayAddr = Get-DeployConfigValue "" "LWYLINK_OVERLAY_ADDR" "overlayAddr" "" }
}

function Sync-VersionFiles {
  if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]{0,31}$') {
    throw "版本号只能使用 1-32 位字母、数字、点、下划线、短横线或加号。当前值：$Version"
  }

  Set-TextFileVersion "cmd\lwylink-windows-ui\app.go" 'var AppVersion = "[^"]+"' "var AppVersion = `"$Version`""
  Set-TextFileVersion "cmd\lwylink-windows-ui\wails.json" '"productVersion": "[^"]+"' "`"productVersion`": `"$Version`""
  Set-TextFileVersion "cmd\lwylink-windows-ui\frontend\package.json" '"version": "[^"]+"' "`"version`": `"$Version`"" 1
  Set-TextFileVersion "cmd\lwylink-windows-ui\frontend\package-lock.json" '"version": "[^"]+"' "`"version`": `"$Version`"" 2
  Set-TextFileVersion "cmd\lwylink-windows-ui\frontend\src\types.ts" 'currentVersion: "[^"]+"' "currentVersion: `"$Version`""
  Set-TextFileVersion "internal\web\admin_assets\admin.html" 'name="version" placeholder="例如 [^"]+"' "name=`"version`" placeholder=`"例如 $Version`""
}

function Read-AdminToken {
  if ($AdminToken -ne "") {
    return $AdminToken
  }
  if ($env:LWYLINK_ADMIN_TOKEN) {
    return $env:LWYLINK_ADMIN_TOKEN
  }
  $configPath = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $configPath) {
    return ((Get-Content $configPath -Raw) | ConvertFrom-Json).adminToken
  }
  return ""
}

function Get-Artifact {
  param([string]$Name)
  if (-not (Test-Path $artifactManifest)) {
    throw "缺少构建产物清单：$artifactManifest"
  }
  $items = Get-Content $artifactManifest -Raw | ConvertFrom-Json
  $item = $items | Where-Object { $_.name -eq $Name } | Select-Object -First 1
  if (-not $item) {
    throw "产物清单中没有 $Name"
  }
  return $item
}

function Normalize-ReleaseBaseURL {
  param(
    [string]$Value,
    [string]$Source
  )
  $raw = $Value.Trim()
  if ($raw -eq "") {
    throw "$Source 不能为空。"
  }
  if ($raw -notmatch '^https?://') {
    $raw = "http://$raw"
  }
  try {
    $uri = [System.Uri]::new($raw)
  } catch {
    throw "$Source 不是合法的 HTTP URL：$Value"
  }
  if (($uri.Scheme -ne "http" -and $uri.Scheme -ne "https") -or $uri.Host -eq "") {
    throw "$Source 必须是 http/https URL：$Value"
  }
  return $raw.TrimEnd("/")
}

function Resolve-ReleaseApiURL {
  if (-not [string]::IsNullOrWhiteSpace($ApiURL)) {
    return Normalize-ReleaseBaseURL $ApiURL "ApiURL"
  }
  if (-not [string]::IsNullOrWhiteSpace($env:LWYLINK_API_URL)) {
    return Normalize-ReleaseBaseURL $env:LWYLINK_API_URL "LWYLINK_API_URL"
  }
  $hostForURL = $HostName
  if ([string]::IsNullOrWhiteSpace($hostForURL)) {
    return $defaultPublicApiURL
  }
  $hostForURL = $hostForURL.Trim()
  if ($hostForURL -match '^https?://') {
    return Normalize-ReleaseBaseURL $hostForURL "HostName"
  }
  return $defaultPublicApiURL
}

function Publish-ClientRelease {
  $token = Read-AdminToken
  if ($token -eq "") {
    throw "缺少 admin token。请设置 -AdminToken 或 LWYLINK_ADMIN_TOKEN，或提供 configs\control.prod.json。"
  }
  if (-not (Test-Path $clientExe)) {
    throw "缺少 Windows 客户端产物：$clientExe"
  }
  $api = Resolve-ReleaseApiURL

  Add-Type -AssemblyName System.Net.Http
  $handler = [System.Net.Http.HttpClientHandler]::new()
  $handler.AllowAutoRedirect = $false
  $client = [System.Net.Http.HttpClient]::new($handler)
  $content = [System.Net.Http.MultipartFormDataContent]::new()
  $file = Get-Item $clientExe
  $fs = [System.IO.File]::OpenRead($file.FullName)
  try {
    $content.Add([System.Net.Http.StringContent]::new([string]$token), "token")
    $content.Add([System.Net.Http.StringContent]::new($Version), "version")
    $content.Add([System.Net.Http.StringContent]::new("windows-amd64"), "platform")
    $content.Add([System.Net.Http.StringContent]::new($Notes), "notes")
    $content.Add([System.Net.Http.StringContent]::new("true"), "mandatory")
    $content.Add([System.Net.Http.StringContent]::new(""), "installArgs")
    $fileContent = [System.Net.Http.StreamContent]::new($fs)
    $fileContent.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse("application/octet-stream")
    $content.Add($fileContent, "file", $file.Name)
    $response = $client.PostAsync("$api/admin/releases/upload", $content).Result
    $status = [int]$response.StatusCode
    $body = $response.Content.ReadAsStringAsync().Result
    if ($status -ne 303 -and $status -ne 200) {
      throw "客户端发布失败 status=$status body=$body"
    }
  } finally {
    $fs.Dispose()
    $content.Dispose()
    $client.Dispose()
    $handler.Dispose()
  }

  $artifact = Get-Artifact "windows-amd64/LwyLink.exe"
  $latest = Invoke-RestMethod -Uri "$api/api/client/releases/latest?platform=windows-amd64" -Method Get
  if ($latest.version -ne $Version) {
    throw "latest 版本校验失败：got $($latest.version), want $Version"
  }
  if ($latest.sha256 -ne $artifact.sha256) {
    throw "latest SHA256 校验失败：got $($latest.sha256), want $($artifact.sha256)"
  }
  Write-Host "客户端发布完成：$($latest.version) $($latest.sha256)" -ForegroundColor Green
}

function Invoke-GitCommit {
  $status = git status --porcelain
  if ($LASTEXITCODE -ne 0) {
    throw "git status failed"
  }
  if ($status -eq "") {
    Write-Host "没有需要提交的改动。" -ForegroundColor Yellow
    return
  }
  $untracked = $status | Where-Object { $_.StartsWith("?? ") }
  if ($untracked) {
    throw "存在未跟踪文件，发布提交已停止。请先确认这些文件是否应加入 Git：`n$($untracked -join "`n")"
  }
  git add -u
  if ($LASTEXITCODE -ne 0) { throw "git add failed" }
  git commit -m "release: publish $Version"
  if ($LASTEXITCODE -ne 0) { throw "git commit failed" }
}

Set-Location $repoRoot
Resolve-DeploySettings

if ($ValidateReleaseApiURL) {
  Resolve-ReleaseApiURL | Write-Output
  return
}

Invoke-Step "同步版本号 $Version" {
  Sync-VersionFiles
}

if (-not $SkipBuild) {
  Invoke-Step "全量构建" {
    $args = @("-File", ".\deploy\build-all.ps1", "-AppVersion", $Version)
    if ($SkipTests) { $args += "-SkipTests" }
    if (-not $NoVet) { $args += "-Vet" }
    if ($NoInstallWails) { $args += "-NoInstallWails" }
    powershell -NoProfile -ExecutionPolicy Bypass @args
    if ($LASTEXITCODE -ne 0) {
      throw "构建失败"
    }
  }
} else {
  Write-Host "`n==> 跳过构建，复用已有产物" -ForegroundColor Cyan
}

if (-not $SkipDeploy) {
  if (-not $SkipBackup) {
    Invoke-Step "发布前远端备份" {
      $args = @("-File", ".\deploy\backup-remote.ps1", "-RemoteRoot", $RemoteRoot, "-RetentionDays", "$BackupRetentionDays")
      if ($HostName -ne "") { $args += @("-HostName", $HostName) }
      if ($User -ne "") { $args += @("-User", $User) }
      if ($Port -ne 0) { $args += @("-Port", "$Port") }
      if ($Password -ne "") { $args += @("-Password", $Password) }
      if ($BackupRoot -ne "") { $args += @("-BackupRoot", $BackupRoot) }
      if ($InstallDailyBackup) { $args += "-InstallDailyTimer" }
      powershell -NoProfile -ExecutionPolicy Bypass @args
      if ($LASTEXITCODE -ne 0) {
        throw "发布前备份失败"
      }
    }
  } else {
    Write-Host "`n==> 跳过发布前备份" -ForegroundColor Cyan
  }

  Invoke-Step "部署并重启服务端" {
    $args = @("-File", ".\deploy\upload-deploy.ps1", "-RemoteRoot", $RemoteRoot)
    if ($HostName -ne "") { $args += @("-HostName", $HostName) }
    if ($User -ne "") { $args += @("-User", $User) }
    if ($Port -ne 0) { $args += @("-Port", "$Port") }
    if ($Password -ne "") { $args += @("-Password", $Password) }
    if ($UploadConfigs) { $args += "-UploadConfigs" }
    if ($SkipSmoke) { $args += "-SkipSmoke" }
    if ($AdminToken -ne "") { $args += @("-AdminToken", $AdminToken) }
    if ($ApiURL -ne "") { $args += @("-ApiURL", $ApiURL) }
    if ($OverlayAddr -ne "") { $args += @("-OverlayAddr", $OverlayAddr) }
    powershell -NoProfile -ExecutionPolicy Bypass @args
    if ($LASTEXITCODE -ne 0) {
      throw "部署失败"
    }
  }
}

if (-not $SkipClientRelease) {
  Invoke-Step "发布 Windows 客户端" {
    Publish-ClientRelease
  }
}

if (-not $SkipDeploy -and -not $SkipClientRelease -and -not $SkipPostReleaseCheck) {
  Invoke-Step "发布后自动巡检" {
    $args = @("-File", ".\deploy\post-release-check.ps1", "-ExpectedVersion", $Version)
    if ($HostName -ne "") { $args += @("-HostName", $HostName) }
    if ($User -ne "") { $args += @("-User", $User) }
    if ($Port -ne 0) { $args += @("-Port", "$Port") }
    if ($Password -ne "") { $args += @("-Password", $Password) }
    if ($ApiURL -ne "") { $args += @("-ApiURL", $ApiURL) }
    if ($OverlayAddr -ne "") { $args += @("-OverlayAddr", $OverlayAddr) }
    if ($AdminToken -ne "") { $args += @("-AdminToken", $AdminToken) }
    if ($SkipSmoke) { $args += "-SkipSmoke" }
    powershell -NoProfile -ExecutionPolicy Bypass @args
    if ($LASTEXITCODE -ne 0) {
      throw "发布后自动巡检失败"
    }
  }
}

if ($Commit) {
  Invoke-Step "提交 git" {
    Invoke-GitCommit
  }
}

Write-Host "`n发布流程完成：$Version" -ForegroundColor Green

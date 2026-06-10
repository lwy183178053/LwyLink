param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$ApiURL = "",
  [string]$OverlayAddr = "",
  [string]$AdminToken = "",
  [string]$Platform = "windows-amd64",
  [string]$RemoteRoot = "",
  [string]$BackupRoot = "",
  [int]$MaxBackupAgeHours = 30,
  [switch]$SkipRemote,
  [switch]$SkipMetrics,
  [switch]$Json
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$deployLocalConfigPath = Join-Path $repoRoot "configs\deploy.local.json"
$deployLocalConfig = $null
if (Test-Path $deployLocalConfigPath) {
  $deployLocalConfig = Get-Content $deployLocalConfigPath -Raw | ConvertFrom-Json
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

function Add-QueryParameter {
  param(
    [string]$URL,
    [string]$Name,
    [string]$Value
  )
  if ([string]::IsNullOrWhiteSpace($Value)) { return $URL }
  $separator = "?"
  if ($URL.Contains("?")) { $separator = "&" }
  return $URL + $separator + [System.Uri]::EscapeDataString($Name) + "=" + [System.Uri]::EscapeDataString($Value)
}

function Invoke-JSON {
  param([string]$URL)
  $response = Invoke-WebRequest -Uri $URL -UseBasicParsing -TimeoutSec 20
  if ($response.StatusCode -lt 200 -or $response.StatusCode -ge 300) {
    throw "HTTP $($response.StatusCode): $URL"
  }
  return $response.Content | ConvertFrom-Json
}

function Invoke-Text {
  param([string]$URL)
  $response = Invoke-WebRequest -Uri $URL -UseBasicParsing -TimeoutSec 20
  if ($response.StatusCode -lt 200 -or $response.StatusCode -ge 300) {
    throw "HTTP $($response.StatusCode): $URL"
  }
  return [string]$response.Content
}

function Get-PrometheusMetricValue {
  param(
    [string]$MetricsText,
    [string]$Name
  )
  $escaped = [regex]::Escape($Name)
  $matches = [regex]::Matches($MetricsText, "(?m)^$escaped(?:\{[^`r`n}]*\})?\s+([-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?)\s*(?:$|#)")
  if ($matches.Count -eq 0) { return $null }
  return [double]::Parse($matches[0].Groups[1].Value, [System.Globalization.CultureInfo]::InvariantCulture)
}

function Read-AdminToken {
  if (-not [string]::IsNullOrWhiteSpace($AdminToken)) { return $AdminToken }
  if (-not [string]::IsNullOrWhiteSpace($env:LWYLINK_ADMIN_TOKEN)) { return $env:LWYLINK_ADMIN_TOKEN }
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    return [string](((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken)
  }
  return ""
}

function New-DeployClient {
  if ($Password -ne "") {
    Import-Module Posh-SSH -ErrorAction Stop
    $securePassword = ConvertTo-SecureString $Password -AsPlainText -Force
    $cred = New-Object System.Management.Automation.PSCredential($User, $securePassword)
    return [pscustomobject]@{ mode = "posh"; credential = $cred }
  }
  return [pscustomobject]@{ mode = "native"; credential = $null }
}

function Invoke-Remote {
  param(
    [object]$Client,
    [string]$Command
  )
  if ($Client.mode -eq "posh") {
    $session = New-SSHSession -ComputerName $HostName -Port $Port -Credential $Client.credential -AcceptKey
    try {
      $result = Invoke-SSHCommand -SessionId $session.SessionId -Command $Command -TimeOut 30
      if ($result.ExitStatus -ne 0) {
        $errText = [string]::Join("`n", @($result.Error))
        throw "远端命令失败：$Command`n$errText"
      }
      return [string]::Join("`n", @($result.Output))
    } finally {
      Remove-SSHSession -SessionId $session.SessionId | Out-Null
    }
  }
  $target = "${User}@${HostName}"
  if ($Command.Contains("`n")) {
    $output = $Command | & ssh -p $Port -o BatchMode=yes -o ConnectTimeout=8 $target "bash -se" 2>&1
  } else {
    $output = & ssh -p $Port -o BatchMode=yes -o ConnectTimeout=8 $target $Command 2>&1
  }
  if ($LASTEXITCODE -ne 0) {
    throw "远端命令失败：$Command`n$([string]::Join("`n", @($output)))"
  }
  return [string]::Join("`n", @($output))
}

function Read-BackupSummary {
  param([object]$Client)
  if ($MaxBackupAgeHours -lt 1) {
    throw "MaxBackupAgeHours 必须大于 0。"
  }
  if ($BackupRoot -notmatch '^/[A-Za-z0-9_./-]+$') {
    throw "BackupRoot 只能使用简单 Linux 绝对路径。当前值：$BackupRoot"
  }
  if ($RemoteRoot -notmatch '^/[A-Za-z0-9_./-]+$') {
    throw "RemoteRoot 只能使用简单 Linux 绝对路径。当前值：$RemoteRoot"
  }
  $maxAgeSeconds = [int64]$MaxBackupAgeHours * 3600
  $remoteCommand = @"
set -e
backup_root='$BackupRoot'
remote_root='$RemoteRoot'
max_age_seconds='$maxAgeSeconds'
if [ ! -d "`$backup_root" ]; then
  echo 'status=missing'
  exit 0
fi
latest=`$(find "`$backup_root" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | head -n 1 | cut -d' ' -f2-)
if [ -z "`$latest" ]; then
  echo 'status=empty'
  exit 0
fi
now=`$(date +%s)
mtime=`$(stat -c %Y "`$latest")
age=`$((now - mtime))
manifest_lines=0
if [ -s "`$latest/manifest.sha256" ]; then
  manifest_lines=`$(wc -l < "`$latest/manifest.sha256")
fi
status='ok'
if [ "`$manifest_lines" -lt 1 ]; then status='bad_manifest'; fi
if [ "`$age" -gt "`$max_age_seconds" ]; then status='old'; fi
runtime_data='missing'
runtime_lines=0
client_releases='absent'
offset_count=0
if [ -s "`$latest/runtime-data.tgz" ] && [ -s "`$latest/runtime-data.files" ]; then
  runtime_data='ok'
  runtime_lines=`$(wc -l < "`$latest/runtime-data.files")
fi
if [ -d "`$remote_root/data/client-releases" ]; then
  client_releases='present'
  if [ "`$runtime_data" = 'ok' ] && ! grep -qx 'data/client-releases' "`$latest/runtime-data.files"; then
    runtime_data='missing_client_releases'
  fi
fi
if [ -d "`$remote_root/data" ]; then
  offset_count=`$(find "`$remote_root/data" -maxdepth 1 -type f \( -name '*.offset' -o -name 'coturn-usage.offset' \) | wc -l)
  if [ "`$offset_count" -gt 0 ] && { [ "`$runtime_data" != 'ok' ] || ! grep -Eq '^data/.*\.offset$|^data/coturn-usage\.offset$' "`$latest/runtime-data.files"; }; then
    runtime_data='missing_offset'
  fi
fi
printf 'status=%s\nlatest=%s\nage_seconds=%s\nmanifest_lines=%s\nruntime_data=%s\nruntime_lines=%s\nclient_releases=%s\noffset_count=%s\n' "`$status" "`$latest" "`$age" "`$manifest_lines" "`$runtime_data" "`$runtime_lines" "`$client_releases" "`$offset_count"
"@
  $result = Invoke-Remote $Client $remoteCommand
  $out = @{}
  foreach ($line in ($result -split "`r?`n")) {
    $idx = $line.IndexOf("=")
    if ($idx -gt 0) {
      $out[$line.Substring(0, $idx)] = $line.Substring($idx + 1)
    }
  }
  return [pscustomobject]@{
    Status        = [string]$out.status
    Latest        = [string]$out.latest
    AgeSeconds    = if ($out.age_seconds) { [int64]$out.age_seconds } else { $null }
    ManifestLines = if ($out.manifest_lines) { [int]$out.manifest_lines } else { $null }
    RuntimeData   = [string]$out.runtime_data
    RuntimeLines  = if ($out.runtime_lines) { [int]$out.runtime_lines } else { $null }
    ClientReleases = [string]$out.client_releases
    OffsetCount   = if ($out.offset_count) { [int]$out.offset_count } else { $null }
  }
}

function Get-ServiceStatus {
  param(
    [object]$Client,
    [string]$Name
  )
  try {
    $status = (Invoke-Remote $Client "systemctl is-active $Name").Trim()
    return $status
  } catch {
    return "unknown"
  }
}

$HostName = Get-DeployConfigValue $HostName "LWYLINK_DEPLOY_HOST" "hostName" ""
$User = Get-DeployConfigValue $User "LWYLINK_DEPLOY_SSH_USER" "user" "root"
if ($Port -eq 0) {
  $portValue = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PORT" "port" ""
  if ($portValue -ne "") { $Port = [int]$portValue }
}
if ($Port -eq 0) { $Port = 22 }
$Password = Get-DeployConfigValue $Password "LWYLINK_DEPLOY_SSH_PASSWORD" "password" ""
$ApiURL = (Get-DeployConfigValue $ApiURL "LWYLINK_API_URL" "apiURL" "https://www.lwylink.xyz").TrimEnd("/")
$OverlayAddr = Get-DeployConfigValue $OverlayAddr "LWYLINK_OVERLAY_ADDR" "overlayAddr" "${HostName}:35768"
$RemoteRoot = Get-DeployConfigValue $RemoteRoot "LWYLINK_REMOTE_ROOT" "remoteRoot" "/opt/lwylink"
$BackupRoot = Get-DeployConfigValue $BackupRoot "LWYLINK_BACKUP_ROOT" "backupRoot" "$RemoteRoot/backups"
$token = Read-AdminToken

$report = [ordered]@{
  GeneratedAt = (Get-Date).ToString("s")
  ApiURL = $ApiURL
  OverlayAddr = $OverlayAddr
  Health = "unknown"
  Latest = $null
  Metrics = $null
  Services = $null
  Backup = $null
  Errors = @()
}

try {
  $health = Invoke-JSON "$ApiURL/healthz"
  $report.Health = if ($health.ok) { "ok" } else { "bad" }
} catch {
  $report.Health = "bad"
  $report.Errors += "healthz: $($_.Exception.Message)"
}

try {
  $latestPlatform = [System.Uri]::EscapeDataString($Platform)
  $latest = Invoke-JSON "$ApiURL/api/client/releases/latest?platform=$latestPlatform"
  $report.Latest = [ordered]@{
    Version = [string]$latest.version
    Platform = [string]$latest.platform
    Mandatory = [bool]$latest.mandatory
    SizeBytes = [int64]$latest.sizeBytes
    SHA256 = [string]$latest.sha256
    DownloadURL = [string]$latest.downloadURL
  }
} catch {
  $report.Errors += "latest: $($_.Exception.Message)"
}

if (-not $SkipMetrics) {
  try {
    $metricText = Invoke-Text "$ApiURL/metrics"
    $report.Metrics = [ordered]@{
      Users = Get-PrometheusMetricValue $metricText "lwylink_users_total"
      ActiveUsers = Get-PrometheusMetricValue $metricText "lwylink_users_active"
      Rooms = Get-PrometheusMetricValue $metricText "lwylink_rooms_total"
      OpenRooms = Get-PrometheusMetricValue $metricText "lwylink_rooms_open"
      OnlineRooms = Get-PrometheusMetricValue $metricText "lwylink_rooms_online"
      OverlayConnections = Get-PrometheusMetricValue $metricText "lwylink_overlay_connections"
      NodesTotal = Get-PrometheusMetricValue $metricText "lwylink_nodes_total"
      NodesDown = Get-PrometheusMetricValue $metricText "lwylink_nodes_down"
      TurnChargeFailures = Get-PrometheusMetricValue $metricText "lwylink_turn_charge_failures_total"
      CoturnCollectorEnabled = Get-PrometheusMetricValue $metricText "lwylink_coturn_usage_collector_enabled"
      CoturnLogAvailable = Get-PrometheusMetricValue $metricText "lwylink_coturn_usage_log_available"
      CoturnLogLagSeconds = Get-PrometheusMetricValue $metricText "lwylink_coturn_log_lag_seconds"
      DownloadsLastDay = Get-PrometheusMetricValue $metricText "lwylink_downloads_last_day"
    }
  } catch {
    $report.Errors += "metrics: $($_.Exception.Message)"
  }
}

if (-not $SkipRemote) {
  if ([string]::IsNullOrWhiteSpace($HostName)) {
    $report.Errors += "remote: HostName 为空，已跳过远端 systemd/backup 检查"
  } else {
    try {
      $client = New-DeployClient
      $report.Services = [ordered]@{
        Control = Get-ServiceStatus $client "lwylink-control"
        Node = Get-ServiceStatus $client "lwylink-node"
        Coturn = Get-ServiceStatus $client "coturn"
        BackupTimer = Get-ServiceStatus $client "lwylink-backup.timer"
      }
      $report.Backup = Read-BackupSummary $client
    } catch {
      $report.Errors += "remote: $($_.Exception.Message)"
    }
  }
}

if ($Json) {
  $report | ConvertTo-Json -Depth 6
  return
}

Write-Host "LwyLink 线上状态报告" -ForegroundColor Cyan
Write-Host "时间：$($report.GeneratedAt)"
Write-Host "API：$($report.ApiURL)"
Write-Host "健康：$($report.Health)"
if ($report.Latest) {
  Write-Host "客户端：$($report.Latest.Version) platform=$($report.Latest.Platform) mandatory=$($report.Latest.Mandatory) size=$($report.Latest.SizeBytes) sha=$($report.Latest.SHA256)"
}
if ($report.Metrics) {
  Write-Host "房间：total=$($report.Metrics.Rooms) open=$($report.Metrics.OpenRooms) online=$($report.Metrics.OnlineRooms) overlay=$($report.Metrics.OverlayConnections)"
  Write-Host "用户：total=$($report.Metrics.Users) active=$($report.Metrics.ActiveUsers)"
  Write-Host "节点/TURN：nodesTotal=$($report.Metrics.NodesTotal) nodesDown=$($report.Metrics.NodesDown) turnChargeFailures=$($report.Metrics.TurnChargeFailures) coturnEnabled=$($report.Metrics.CoturnCollectorEnabled) coturnLog=$($report.Metrics.CoturnLogAvailable) lag=$($report.Metrics.CoturnLogLagSeconds)s"
  Write-Host "下载：lastDay=$($report.Metrics.DownloadsLastDay)"
}
if ($report.Services) {
  Write-Host "服务：control=$($report.Services.Control) node=$($report.Services.Node) coturn=$($report.Services.Coturn) backupTimer=$($report.Services.BackupTimer)"
}
if ($report.Backup) {
  $ageHours = "-"
  if ($null -ne $report.Backup.AgeSeconds) {
    $ageHours = [math]::Round($report.Backup.AgeSeconds / 3600, 2)
  }
  Write-Host "备份：status=$($report.Backup.Status) latest=$($report.Backup.Latest) ageHours=$ageHours manifestLines=$($report.Backup.ManifestLines) runtime=$($report.Backup.RuntimeData) runtimeLines=$($report.Backup.RuntimeLines) clientReleases=$($report.Backup.ClientReleases) offsets=$($report.Backup.OffsetCount)"
}
if ($report.Errors.Count -gt 0) {
  Write-Host "问题：" -ForegroundColor Yellow
  foreach ($err in $report.Errors) {
    Write-Host " - $err" -ForegroundColor Yellow
  }
}

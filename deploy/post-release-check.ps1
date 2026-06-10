param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$ApiURL = "",
  [string]$OverlayAddr = "",
  [string]$AdminToken = "",
  [string]$ExpectedVersion = "",
  [string]$ExpectedSHA256 = "",
  [string]$RemoteRoot = "",
  [string]$BackupRoot = "",
  [int]$MaxBackupAgeHours = 30,
  [switch]$SkipSmoke,
  [switch]$SkipRemote,
  [switch]$SkipCoturn,
  [switch]$SkipBackupTimer,
  [switch]$SkipDownload,
  [switch]$SkipMetrics,
  [switch]$SkipBackupManifest
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

if ($HostName -eq "") { $HostName = Get-DeployConfigValue "" "LWYLINK_DEPLOY_HOST" "hostName" "" }
if ($User -eq "") { $User = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_USER" "user" "root" }
if ($Port -eq 0) {
  $portValue = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PORT" "port" ""
  if ($portValue -ne "") { $Port = [int]$portValue }
}
if ($Port -eq 0) { $Port = 22 }
if ($Password -eq "") { $Password = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PASSWORD" "password" "" }
if ($ApiURL -eq "") { $ApiURL = Get-DeployConfigValue "" "LWYLINK_API_URL" "apiURL" "https://www.lwylink.xyz" }
if ($OverlayAddr -eq "") { $OverlayAddr = Get-DeployConfigValue "" "LWYLINK_OVERLAY_ADDR" "overlayAddr" "${HostName}:35768" }
if ($RemoteRoot -eq "") { $RemoteRoot = Get-DeployConfigValue "" "LWYLINK_REMOTE_ROOT" "remoteRoot" "/opt/lwylink" }
if ($BackupRoot -eq "") { $BackupRoot = Get-DeployConfigValue "" "LWYLINK_BACKUP_ROOT" "backupRoot" "$RemoteRoot/backups" }
if ($AdminToken -eq "") { $AdminToken = $env:LWYLINK_ADMIN_TOKEN }
if ($AdminToken -eq "") {
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    $AdminToken = ((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken
  }
}

function Invoke-Check {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
  Write-Host "OK $Title" -ForegroundColor Green
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

function Normalize-SHA256 {
  param([string]$Value)
  $sha = ([string]$Value).Trim().ToLowerInvariant()
  if ($sha -eq "") { return "" }
  if ($sha -notmatch '^[0-9a-f]{64}$') {
    throw "SHA256 格式非法：$Value"
  }
  return $sha
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

function Save-DownloadFile {
  param(
    [string]$URL,
    [string]$Destination
  )
  try {
    Invoke-WebRequest -Uri $URL -UseBasicParsing -TimeoutSec 120 -OutFile $Destination
  } catch {
    throw "下载 latest 文件失败：$($_.Exception.Message)"
  }
  if (-not (Test-Path $Destination)) {
    throw "下载 latest 文件后未找到本地临时文件。"
  }
  $size = (Get-Item $Destination).Length
  if ($size -le 0) {
    throw "下载 latest 文件为空。"
  }
  return $size
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

function Assert-PrometheusMetric {
  param(
    [string]$MetricsText,
    [string]$Name
  )
  $value = Get-PrometheusMetricValue $MetricsText $Name
  if ($null -eq $value) {
    throw "/metrics 缺少关键指标：$Name"
  }
  return $value
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
      if ($result.Output) { $result.Output | ForEach-Object { Write-Host $_ } }
      if ($result.Error) { $result.Error | ForEach-Object { Write-Host $_ -ForegroundColor Yellow } }
      if ($result.ExitStatus -ne 0) { throw "远端命令失败：$Command" }
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
  if ($output) { $output | ForEach-Object { Write-Host $_ } }
  if ($LASTEXITCODE -ne 0) { throw "远端命令失败：$Command" }
  return [string]::Join("`n", @($output))
}

function Assert-RemotePathSafe {
  param(
    [string]$Path,
    [string]$Name
  )
  if ($Path -notmatch '^/[A-Za-z0-9_./-]+$') {
    throw "$Name 只能使用简单 Linux 绝对路径。当前值：$Path"
  }
}

$latestRelease = $null
$metricsText = ""

Invoke-Check "healthz" {
  $health = Invoke-JSON "$ApiURL/healthz"
  if (-not $health.ok) { throw "healthz ok=false" }
}

Invoke-Check "latest release" {
  $release = Invoke-JSON "$ApiURL/api/client/releases/latest?platform=windows-amd64"
  if ([string]::IsNullOrWhiteSpace([string]$release.version) -or [string]::IsNullOrWhiteSpace([string]$release.downloadURL)) {
    throw "latest release missing version or downloadURL"
  }
  if (-not [string]::IsNullOrWhiteSpace($ExpectedVersion) -and [string]$release.version -ne $ExpectedVersion) {
    throw "latest release version mismatch: got $($release.version), want $ExpectedVersion"
  }
  $releaseSHA = Normalize-SHA256 ([string]$release.sha256)
  $expectedReleaseSHA = Normalize-SHA256 $ExpectedSHA256
  if ($expectedReleaseSHA -ne "" -and $releaseSHA -ne $expectedReleaseSHA) {
    throw "latest release sha256 mismatch: got $releaseSHA, want $expectedReleaseSHA"
  }
  $script:latestRelease = $release
  Write-Host "latest windows=$($release.version) sha256=$releaseSHA"
}

if (-not $SkipDownload) {
  Invoke-Check "latest download" {
    if (-not $latestRelease) {
      throw "缺少 latest release 结果，无法检查下载 URL。"
    }
    $downloadURL = [string]$latestRelease.downloadURL
    if ([string]::IsNullOrWhiteSpace($downloadURL)) {
      throw "latest release downloadURL 为空。"
    }
    $checkURL = Add-QueryParameter $downloadURL "token" $AdminToken
    $expectedSHA = Normalize-SHA256 $ExpectedSHA256
    if ($expectedSHA -eq "") {
      $expectedSHA = Normalize-SHA256 ([string]$latestRelease.sha256)
    }
    $tmp = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), "lwylink-latest-" + [System.Guid]::NewGuid().ToString("N") + ".bin")
    try {
      $size = Save-DownloadFile $checkURL $tmp
      if ($latestRelease.PSObject.Properties.Name -contains "sizeBytes" -and [int64]$latestRelease.sizeBytes -gt 0 -and $size -ne [int64]$latestRelease.sizeBytes) {
        throw "latest 下载文件大小不匹配：got $size, want $($latestRelease.sizeBytes)"
      }
      if ($expectedSHA -ne "") {
        $actualSHA = (Get-FileHash -Path $tmp -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actualSHA -ne $expectedSHA) {
          throw "latest 下载文件 SHA256 不匹配：got $actualSHA, want $expectedSHA"
        }
        Write-Host "latest 下载可访问，大小=$size，SHA256 已校验。"
      } else {
        Write-Host "latest 下载可访问，大小=$size；未提供 SHA256，跳过哈希校验。" -ForegroundColor Yellow
      }
    } finally {
      Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
    }
  }
} else {
  Write-Host "`n==> 跳过 latest download" -ForegroundColor Yellow
}

if (-not $SkipMetrics) {
  Invoke-Check "metrics" {
    $script:metricsText = Invoke-Text "$ApiURL/metrics"
    $requiredMetrics = @(
      "lwylink_users_total",
      "lwylink_rooms_total",
      "lwylink_overlay_connections",
      "lwylink_turn_charge_failures_total",
      "lwylink_coturn_usage_collector_enabled",
      "lwylink_coturn_usage_log_available",
      "lwylink_coturn_log_lag_seconds",
      "lwylink_nodes_down",
      "lwylink_downloads_last_day"
    )
    foreach ($metricName in $requiredMetrics) {
      [void](Assert-PrometheusMetric $metricsText $metricName)
    }
    Write-Host "/metrics 可访问，关键指标 $($requiredMetrics.Count) 项齐全。"
  }

  if (-not $SkipCoturn) {
    Invoke-Check "coturn collector metrics" {
      if ([string]::IsNullOrWhiteSpace($metricsText)) {
        throw "缺少 /metrics 内容，无法判断 coturn collector。"
      }
      $enabled = Assert-PrometheusMetric $metricsText "lwylink_coturn_usage_collector_enabled"
      $available = Assert-PrometheusMetric $metricsText "lwylink_coturn_usage_log_available"
      $lag = Assert-PrometheusMetric $metricsText "lwylink_coturn_log_lag_seconds"
      if ($enabled -lt 1) {
        throw "coturn collector 未启用：lwylink_coturn_usage_collector_enabled=$enabled"
      }
      if ($available -lt 1) {
        throw "coturn usage 日志不可读：lwylink_coturn_usage_log_available=$available"
      }
      if ($lag -lt 0) {
        throw "coturn 日志延迟指标异常：lwylink_coturn_log_lag_seconds=$lag"
      }
      Write-Host "coturn collector metrics 正常，log lag=${lag}s。"
    }
  }
} else {
  Write-Host "`n==> 跳过 metrics" -ForegroundColor Yellow
}

if (-not $SkipSmoke) {
  Invoke-Check "smoke" {
    $args = @("run", ".\cmd\lwylink-smoke", "-api", $ApiURL, "-overlay", $OverlayAddr)
    if (-not [string]::IsNullOrWhiteSpace($AdminToken)) {
      $args += @("-admin-token", $AdminToken)
    }
    Push-Location $repoRoot
    try {
      & go @args
      if ($LASTEXITCODE -ne 0) { throw "smoke failed" }
    } finally {
      Pop-Location
    }
  }
}

if (-not $SkipRemote) {
  $shouldCheckBackupManifest = (-not $SkipBackupTimer -and -not $SkipBackupManifest)
  if ((-not $SkipCoturn -or -not $SkipBackupTimer -or $shouldCheckBackupManifest) -and [string]::IsNullOrWhiteSpace($HostName)) {
    throw "缺少 HostName，无法检查 coturn、backup timer 或备份 manifest。"
  }
  $client = $null
  if (-not $SkipCoturn -or -not $SkipBackupTimer -or $shouldCheckBackupManifest) {
    $client = New-DeployClient
  }
  if (-not $SkipCoturn) {
    Invoke-Check "coturn active" {
      Invoke-Remote $client "systemctl is-active --quiet coturn"
    }
  } else {
    Write-Host "`n==> 跳过 coturn active" -ForegroundColor Yellow
  }
  if (-not $SkipBackupTimer) {
    Invoke-Check "backup timer active" {
      Invoke-Remote $client "systemctl is-active --quiet lwylink-backup.timer"
    }
  } else {
    Write-Host "`n==> 跳过 backup timer active" -ForegroundColor Yellow
  }
  if ($shouldCheckBackupManifest) {
    Invoke-Check "backup manifest" {
      if ($MaxBackupAgeHours -lt 1) {
        throw "MaxBackupAgeHours 必须大于 0。"
      }
      Assert-RemotePathSafe $BackupRoot "BackupRoot"
      Assert-RemotePathSafe $RemoteRoot "RemoteRoot"
      $maxAgeSeconds = [int64]$MaxBackupAgeHours * 3600
      $remoteCommand = @"
set -e
backup_root='$BackupRoot'
remote_root='$RemoteRoot'
max_age_seconds='$maxAgeSeconds'
if [ ! -d "`$backup_root" ]; then
  echo "backup root not found: `$backup_root" >&2
  exit 20
fi
latest=`$(find "`$backup_root" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | head -n 1 | cut -d' ' -f2-)
if [ -z "`$latest" ]; then
  echo "no backup directory under `$backup_root" >&2
  exit 21
fi
if [ ! -s "`$latest/manifest.sha256" ]; then
  echo "manifest.sha256 missing or empty: `$latest/manifest.sha256" >&2
  exit 22
fi
now=`$(date +%s)
mtime=`$(stat -c %Y "`$latest")
age=`$((now - mtime))
if [ "`$age" -gt "`$max_age_seconds" ]; then
  echo "backup too old: latest=`$latest age_seconds=`$age max_seconds=`$max_age_seconds" >&2
  exit 23
fi
files=`$(cd "`$latest" && find . -maxdepth 1 -type f ! -name manifest.sha256 | wc -l)
manifest_lines=`$(wc -l < "`$latest/manifest.sha256")
if [ "`$files" -lt 1 ] || [ "`$manifest_lines" -lt 1 ]; then
  echo "backup manifest has no files: `$latest/manifest.sha256" >&2
  exit 24
fi
if [ ! -s "`$latest/runtime-data.tgz" ]; then
  echo "runtime-data.tgz missing or empty: `$latest/runtime-data.tgz" >&2
  exit 25
fi
if [ ! -s "`$latest/runtime-data.files" ]; then
  echo "runtime-data.files missing or empty: `$latest/runtime-data.files" >&2
  exit 26
fi
tar -tzf "`$latest/runtime-data.tgz" >/dev/null
if [ -d "`$remote_root/data/client-releases" ] && ! grep -qx 'data/client-releases' "`$latest/runtime-data.files"; then
  echo "runtime backup missing data/client-releases entry" >&2
  exit 27
fi
offset_count=0
if [ -d "`$remote_root/data" ]; then
  offset_count=`$(find "`$remote_root/data" -maxdepth 1 -type f \( -name '*.offset' -o -name 'coturn-usage.offset' \) | wc -l)
fi
if [ "`$offset_count" -gt 0 ] && ! grep -Eq '^data/.*\.offset$|^data/coturn-usage\.offset$' "`$latest/runtime-data.files"; then
  echo "runtime backup missing TURN offset entry" >&2
  exit 28
fi
runtime_lines=`$(wc -l < "`$latest/runtime-data.files")
printf 'latest=%s\nage_seconds=%s\nmanifest_lines=%s\nruntime_lines=%s\noffset_count=%s\n' "`$latest" "`$age" "`$manifest_lines" "`$runtime_lines" "`$offset_count"
"@
      $result = Invoke-Remote $client $remoteCommand
      $ageMatch = [regex]::Match($result, '(?m)^age_seconds=(\d+)$')
      $latestMatch = [regex]::Match($result, '(?m)^latest=(.+)$')
      if (-not $ageMatch.Success -or -not $latestMatch.Success) {
        throw "无法解析远端备份检查结果。"
      }
      $ageSeconds = [int64]$ageMatch.Groups[1].Value
      $runtimeMatch = [regex]::Match($result, '(?m)^runtime_lines=(\d+)$')
      $runtimeText = if ($runtimeMatch.Success) { "，runtime 清单 $($runtimeMatch.Groups[1].Value) 项" } else { "" }
      Write-Host "最新备份：$($latestMatch.Groups[1].Value)，年龄约 $([math]::Round($ageSeconds / 3600, 2)) 小时$runtimeText。"
    }
  } elseif ($SkipBackupManifest) {
    Write-Host "`n==> 跳过 backup manifest" -ForegroundColor Yellow
  } elseif ($SkipBackupTimer) {
    Write-Host "`n==> 跳过 backup manifest（已跳过 backup timer）" -ForegroundColor Yellow
  }
}

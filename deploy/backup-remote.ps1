param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$RemoteRoot = "",
  [string]$BackupRoot = "",
  [int]$RetentionDays = 14,
  [switch]$SkipDatabase,
  [switch]$SkipRedis,
  [switch]$SkipConfigs,
  [switch]$SkipRuntimeData,
  [switch]$InstallDailyTimer,
  [switch]$ValidateOnly
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

function Resolve-BackupSettings {
  if ($HostName -eq "") { $script:HostName = Get-DeployConfigValue "" "LWYLINK_DEPLOY_HOST" "hostName" "" }
  if ($User -eq "") { $script:User = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_USER" "user" "root" }
  if ($Port -eq 0) {
    $portValue = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PORT" "port" ""
    if ($portValue -ne "") { $script:Port = [int]$portValue }
  }
  if ($Port -eq 0) { $script:Port = 22 }
  if ($Password -eq "") { $script:Password = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PASSWORD" "password" "" }
  if ($RemoteRoot -eq "") { $script:RemoteRoot = Get-DeployConfigValue "" "LWYLINK_REMOTE_ROOT" "remoteRoot" "/opt/lwylink" }
  if ($BackupRoot -eq "") { $script:BackupRoot = Get-DeployConfigValue "" "LWYLINK_BACKUP_ROOT" "backupRoot" "$RemoteRoot/backups" }

  if ([string]::IsNullOrWhiteSpace($HostName)) {
    throw "缺少部署主机。请设置 -HostName、LWYLINK_DEPLOY_HOST 或 configs\deploy.local.json。"
  }
  foreach ($path in @($RemoteRoot, $BackupRoot)) {
    if ($path -notmatch '^/[A-Za-z0-9_./-]+$') {
      throw "远端路径只能使用简单 Linux 绝对路径。当前值：$path"
    }
  }
  if ($RetentionDays -lt 1 -or $RetentionDays -gt 3650) {
    throw "RetentionDays 必须在 1 到 3650 之间。"
  }
}

function New-DeployClient {
  if ($Password -ne "") {
    Import-Module Posh-SSH -ErrorAction Stop
    $securePassword = ConvertTo-SecureString $Password -AsPlainText -Force
    $cred = New-Object System.Management.Automation.PSCredential($User, $securePassword)
    return [pscustomobject]@{
      mode = "posh"
      credential = $cred
    }
  }

  return [pscustomobject]@{
    mode = "native"
    credential = $null
  }
}

function Assert-DeployAuthReady {
  param([object]$Client)
  if ($Client.mode -eq "posh") {
    return
  }

  $target = "${User}@${HostName}"
  & ssh -p $Port -o BatchMode=yes -o ConnectTimeout=8 $target "true" 2>$null
  if ($LASTEXITCODE -ne 0) {
    throw "SSH 无法非交互登录 $target。请配置 SSH key，或提供密码。"
  }
}

function Invoke-Remote {
  param(
    [object]$Client,
    [string]$Command,
    [int]$TimeOut = 180
  )

  if ($Client.mode -eq "posh") {
    $session = New-SSHSession -ComputerName $HostName -Port $Port -Credential $Client.credential -AcceptKey
    try {
      $result = Invoke-SSHCommand -SessionId $session.SessionId -Command $Command -TimeOut $TimeOut
      if ($result.Output) { $result.Output | ForEach-Object { Write-Host $_ } }
      if ($result.Error) { $result.Error | ForEach-Object { Write-Host $_ -ForegroundColor Yellow } }
      if ($result.ExitStatus -ne 0) {
        throw "远端命令失败，ExitStatus=$($result.ExitStatus)"
      }
    } finally {
      Remove-SSHSession -SessionId $session.SessionId | Out-Null
    }
    return
  }

  $target = "${User}@${HostName}"
  $Command | & ssh -p $Port $target "bash -se"
  if ($LASTEXITCODE -ne 0) {
    throw "远端命令失败，ExitCode=$LASTEXITCODE"
  }
}

function Get-RemoteBackupScript {
  param(
    [string]$Mode
  )
  $skipDatabaseValue = if ($SkipDatabase) { "1" } else { "0" }
  $skipRedisValue = if ($SkipRedis) { "1" } else { "0" }
  $skipConfigsValue = if ($SkipConfigs) { "1" } else { "0" }
  $skipRuntimeDataValue = if ($SkipRuntimeData) { "1" } else { "0" }
  $installTimerValue = if ($InstallDailyTimer) { "1" } else { "0" }

  $backupBody = @'
backup_once() {
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  dest="$BACKUP_ROOT/$stamp"
  mkdir -p "$dest"

  {
    echo "generated_at=$stamp"
    echo "remote_root=$REMOTE_ROOT"
    echo "host=$(hostname)"
  } > "$dest/backup-info.txt"

  if [ "$SKIP_CONFIGS" != "1" ]; then
    config_items=""
    [ -d "$REMOTE_ROOT/configs" ] && config_items="$config_items configs"
    [ -f "$REMOTE_ROOT/deploy/.env" ] && config_items="$config_items deploy/.env"
    [ -f "$REMOTE_ROOT/deploy/docker-compose.prod.yml" ] && config_items="$config_items deploy/docker-compose.prod.yml"
    [ -f "$REMOTE_ROOT/deploy/lwylink-control.service" ] && config_items="$config_items deploy/lwylink-control.service"
    [ -f "$REMOTE_ROOT/deploy/lwylink-node.service" ] && config_items="$config_items deploy/lwylink-node.service"
    if [ -n "$config_items" ]; then
      tar -C "$REMOTE_ROOT" -czf "$dest/configs.tgz" $config_items
    fi
  fi

  if [ "$SKIP_RUNTIME_DATA" != "1" ] && [ -d "$REMOTE_ROOT/data" ]; then
    runtime_list="$dest/runtime-data.files"
    (
      cd "$REMOTE_ROOT"
      {
        [ -d data/client-releases ] && find data/client-releases -mindepth 0 -print
        find data -maxdepth 1 -type f \( -name '*.offset' -o -name 'coturn-usage.offset' \) -print
      } | sort -u
    ) > "$runtime_list"
    if [ -s "$runtime_list" ]; then
      tar -C "$REMOTE_ROOT" -czf "$dest/runtime-data.tgz" -T "$runtime_list"
    else
      rm -f "$runtime_list"
    fi
  fi

  if [ "$SKIP_DATABASE" != "1" ]; then
    if docker ps --format '{{.Names}}' | grep -qx 'lwylink-mysql'; then
      docker exec lwylink-mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -u "$MYSQL_USER" --single-transaction --routines --triggers lwylink' > "$dest/mysql-lwylink.sql"
      gzip -f "$dest/mysql-lwylink.sql"
    else
      echo "lwylink-mysql container not running; mysql backup skipped" >&2
    fi
  fi

  if [ "$SKIP_REDIS" != "1" ]; then
    if docker ps --format '{{.Names}}' | grep -qx 'lwylink-redis'; then
      docker exec lwylink-redis redis-cli SAVE >/dev/null
      docker cp lwylink-redis:/data/dump.rdb "$dest/redis-dump.rdb"
      gzip -f "$dest/redis-dump.rdb"
    else
      echo "lwylink-redis container not running; redis backup skipped" >&2
    fi
  fi

  (cd "$dest" && find . -maxdepth 1 -type f ! -name manifest.sha256 -print0 | sort -z | xargs -0 -r sha256sum > manifest.sha256)
  find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -mtime "+$RETENTION_DAYS" -exec rm -rf {} +
  echo "backup created: $dest"
}
'@

  $template = @'
set -euo pipefail
REMOTE_ROOT='__REMOTE_ROOT__'
BACKUP_ROOT='__BACKUP_ROOT__'
RETENTION_DAYS='__RETENTION_DAYS__'
SKIP_DATABASE='__SKIP_DATABASE__'
SKIP_REDIS='__SKIP_REDIS__'
SKIP_CONFIGS='__SKIP_CONFIGS__'
SKIP_RUNTIME_DATA='__SKIP_RUNTIME_DATA__'
INSTALL_DAILY_TIMER='__INSTALL_DAILY_TIMER__'
__BACKUP_BODY__

install_timer() {
  install -d -m 0755 /usr/local/sbin
  cat > /usr/local/sbin/lwylink-backup <<'EOS_BACKUP'
#!/usr/bin/env bash
set -euo pipefail
REMOTE_ROOT='__REMOTE_ROOT__'
BACKUP_ROOT='__BACKUP_ROOT__'
RETENTION_DAYS='__RETENTION_DAYS__'
SKIP_DATABASE='__SKIP_DATABASE__'
SKIP_REDIS='__SKIP_REDIS__'
SKIP_CONFIGS='__SKIP_CONFIGS__'
SKIP_RUNTIME_DATA='__SKIP_RUNTIME_DATA__'
INSTALL_DAILY_TIMER='0'
__BACKUP_BODY__
backup_once
EOS_BACKUP
  chmod 0755 /usr/local/sbin/lwylink-backup
  cat > /etc/systemd/system/lwylink-backup.service <<'EOS_SERVICE'
[Unit]
Description=LwyLink MySQL/Redis/config backup

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/lwylink-backup
EOS_SERVICE
  cat > /etc/systemd/system/lwylink-backup.timer <<'EOS_TIMER'
[Unit]
Description=Run LwyLink backup daily

[Timer]
OnCalendar=*-*-* 03:20:00
Persistent=true

[Install]
WantedBy=timers.target
EOS_TIMER
  systemctl daemon-reload
  systemctl enable --now lwylink-backup.timer
  systemctl list-timers lwylink-backup.timer --no-pager
}

if [ "$INSTALL_DAILY_TIMER" = "1" ]; then
  install_timer
fi

if [ "__MODE__" = "backup" ]; then
  backup_once
else
  echo "backup script validated"
fi
'@

  $script = $template.Replace("__REMOTE_ROOT__", $RemoteRoot)
  $script = $script.Replace("__BACKUP_ROOT__", $BackupRoot)
  $script = $script.Replace("__RETENTION_DAYS__", [string]$RetentionDays)
  $script = $script.Replace("__SKIP_DATABASE__", $skipDatabaseValue)
  $script = $script.Replace("__SKIP_REDIS__", $skipRedisValue)
  $script = $script.Replace("__SKIP_CONFIGS__", $skipConfigsValue)
  $script = $script.Replace("__SKIP_RUNTIME_DATA__", $skipRuntimeDataValue)
  $script = $script.Replace("__INSTALL_DAILY_TIMER__", $installTimerValue)
  $script = $script.Replace("__MODE__", $Mode)
  $script = $script.Replace("__BACKUP_BODY__", $backupBody)
  return $script
}

Resolve-BackupSettings
$client = New-DeployClient
Assert-DeployAuthReady $client

if ($ValidateOnly) {
  Write-Host "备份脚本预检通过：$User@${HostName}:$Port -> $BackupRoot" -ForegroundColor Green
  return
}

Invoke-Remote $client (Get-RemoteBackupScript "backup") 300

if ($InstallDailyTimer) {
  Write-Host "每日备份 timer 已安装，且已完成本次备份：lwylink-backup.timer" -ForegroundColor Green
} else {
  Write-Host "远端备份完成：$BackupRoot" -ForegroundColor Green
}

param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$RemoteRoot = "",
  [string]$TurnConfig = "",
  [string]$Realm = "",
  [string]$PublicHost = "",
  [string]$StaticAuthSecret = "",
  [int]$ListeningPort = 0,
  [int]$TLSListeningPort = 0,
  [int]$MinPort = 0,
  [int]$MaxPort = 0,
  [string]$UsernamePrefix = "",
  [int]$TTLSeconds = 0,
  [switch]$EnableTLS,
  [string]$CertPath = "",
  [string]$PKeyPath = "",
  [switch]$UpdateLocalControlConfig,
  [switch]$LocalConfigOnly,
  [switch]$RestartControl,
  [switch]$ValidateOnly
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$deployLocalConfigPath = Join-Path $repoRoot "configs\deploy.local.json"
$deployLocalConfig = $null
if (Test-Path $deployLocalConfigPath) {
  $deployLocalConfig = Get-Content $deployLocalConfigPath -Raw | ConvertFrom-Json
}

if ($TurnConfig -eq "") {
  $defaultTurnConfig = Join-Path $repoRoot "configs\turn.local.json"
  if (Test-Path $defaultTurnConfig) {
    $TurnConfig = $defaultTurnConfig
  }
}
$turnLocalConfig = $null
if ($TurnConfig -ne "" -and (Test-Path $TurnConfig)) {
  $turnLocalConfig = Get-Content $TurnConfig -Raw | ConvertFrom-Json
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

function Get-TurnConfigValue {
  param(
    [object]$CliValue,
    [string]$EnvName,
    [string]$ConfigName,
    [object]$DefaultValue
  )
  if ($null -ne $CliValue) {
    if ($CliValue -is [string] -and -not [string]::IsNullOrWhiteSpace($CliValue)) { return $CliValue }
    if ($CliValue -is [int] -and $CliValue -gt 0) { return $CliValue }
    if ($CliValue -is [bool] -and $CliValue) { return $CliValue }
  }
  $envValue = [Environment]::GetEnvironmentVariable($EnvName)
  if (-not [string]::IsNullOrWhiteSpace($envValue)) {
    if ($DefaultValue -is [int]) { return [int]$envValue }
    if ($DefaultValue -is [bool]) { return $envValue -match '^(1|true|yes|on)$' }
    return $envValue
  }
  if ($turnLocalConfig -and $turnLocalConfig.PSObject.Properties.Name -contains $ConfigName) {
    $configValue = $turnLocalConfig.$ConfigName
    if ($null -ne $configValue) {
      if ($configValue -is [string] -and -not [string]::IsNullOrWhiteSpace($configValue)) { return $configValue }
      if ($configValue -is [int] -and $configValue -gt 0) { return $configValue }
      if ($configValue -is [bool]) { return $configValue }
    }
  }
  return $DefaultValue
}

function Invoke-Step {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
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
    throw "SSH 无法非交互登录 $target。请配置 SSH key，或在 configs\deploy.local.json / LWYLINK_DEPLOY_SSH_PASSWORD / -Password 中提供密码。"
  }
}

function Invoke-Remote {
  param(
    [object]$Client,
    [string]$Command,
    [int]$TimeOut = 120
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

function Assert-SimpleHostname {
  param([string]$Name, [string]$Value)
  if ($Value -notmatch '^[A-Za-z0-9.-]+$') {
    throw "$Name 只能使用域名或 IP，当前值：$Value"
  }
}

function Assert-PortRange {
  param([int]$Value, [string]$Name)
  if ($Value -lt 1 -or $Value -gt 65535) {
    throw "$Name 端口不合法：$Value"
  }
}

function Update-ControlConfig {
  param(
    [string]$ConfigPath,
    [string]$TurnURL,
    [string]$TurnsURL,
    [string]$Secret,
    [int]$TTL,
    [string]$Prefix
  )
  if (-not (Test-Path $ConfigPath)) {
    throw "缺少本机控制端配置：$ConfigPath"
  }
  $cfg = Get-Content $ConfigPath -Raw | ConvertFrom-Json
  $urls = @($TurnURL)
  if (-not [string]::IsNullOrWhiteSpace($TurnsURL)) {
    $urls += $TurnsURL
  }
  Set-ObjectProperty -Object $cfg -Name "voiceTURNCredential" -Value ([pscustomobject]@{
    urls = $urls
    staticAuthSecret = $Secret
    ttlSeconds = $TTL
    usernamePrefix = $Prefix
  })
  Set-ObjectProperty -Object $cfg -Name "voiceICEServers" -Value @(
    [pscustomobject]@{
      urls = @("stun:stun.lwylink.xyz:3478")
    }
  )
  $json = $cfg | ConvertTo-Json -Depth 12
  [System.IO.File]::WriteAllText((Resolve-Path $ConfigPath), $json + "`n", [System.Text.UTF8Encoding]::new($false))
}

function Set-ObjectProperty {
  param(
    [object]$Object,
    [string]$Name,
    [object]$Value
  )
  if ($Object.PSObject.Properties.Name -contains $Name) {
    $Object.$Name = $Value
    return
  }
  $Object | Add-Member -NotePropertyName $Name -NotePropertyValue $Value
}

if ($HostName -eq "") { $HostName = Get-DeployConfigValue "" "LWYLINK_DEPLOY_HOST" "hostName" "64.83.34.53" }
if ($User -eq "") { $User = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_USER" "user" "root" }
if ($Port -eq 0) {
  $portValue = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PORT" "port" ""
  if ($portValue -ne "") { $Port = [int]$portValue }
}
if ($Port -eq 0) { $Port = 22 }
if ($Password -eq "") { $Password = Get-DeployConfigValue "" "LWYLINK_DEPLOY_SSH_PASSWORD" "password" "" }
if ($RemoteRoot -eq "") { $RemoteRoot = Get-DeployConfigValue "" "LWYLINK_REMOTE_ROOT" "remoteRoot" "/opt/lwylink" }

$Realm = Get-TurnConfigValue $Realm "LWYLINK_TURN_REALM" "realm" "lwylink.xyz"
$PublicHost = Get-TurnConfigValue $PublicHost "LWYLINK_TURN_PUBLIC_HOST" "publicHost" "www.lwylink.xyz"
$StaticAuthSecret = Get-TurnConfigValue $StaticAuthSecret "LWYLINK_TURN_SECRET" "staticAuthSecret" ""
$ListeningPort = Get-TurnConfigValue $ListeningPort "LWYLINK_TURN_PORT" "listeningPort" 3478
$TLSListeningPort = Get-TurnConfigValue $TLSListeningPort "LWYLINK_TURN_TLS_PORT" "tlsListeningPort" 5349
$MinPort = Get-TurnConfigValue $MinPort "LWYLINK_TURN_MIN_PORT" "minPort" 49160
$MaxPort = Get-TurnConfigValue $MaxPort "LWYLINK_TURN_MAX_PORT" "maxPort" 49200
$UsernamePrefix = Get-TurnConfigValue $UsernamePrefix "LWYLINK_TURN_USERNAME_PREFIX" "usernamePrefix" "lwylink"
$TTLSeconds = Get-TurnConfigValue $TTLSeconds "LWYLINK_TURN_TTL_SECONDS" "ttlSeconds" 3600
$EnableTLS = [bool](Get-TurnConfigValue ([bool]$EnableTLS) "LWYLINK_TURN_ENABLE_TLS" "enableTLS" $false)
$CertPath = Get-TurnConfigValue $CertPath "LWYLINK_TURN_CERT_PATH" "certPath" ""
$PKeyPath = Get-TurnConfigValue $PKeyPath "LWYLINK_TURN_PKEY_PATH" "pkeyPath" ""

Assert-SimpleHostname "Realm" $Realm
Assert-SimpleHostname "PublicHost" $PublicHost
Assert-PortRange $ListeningPort "listeningPort"
Assert-PortRange $TLSListeningPort "tlsListeningPort"
Assert-PortRange $MinPort "minPort"
Assert-PortRange $MaxPort "maxPort"
if ($MinPort -gt $MaxPort) {
  throw "minPort 不能大于 maxPort"
}
if ([string]::IsNullOrWhiteSpace($StaticAuthSecret) -or $StaticAuthSecret.StartsWith("CHANGE_ME")) {
  throw "缺少 TURN staticAuthSecret。请复制 deploy\turn.example.json 到 configs\turn.local.json 后填写，或设置 LWYLINK_TURN_SECRET。"
}
if ($EnableTLS -and ([string]::IsNullOrWhiteSpace($CertPath) -or [string]::IsNullOrWhiteSpace($PKeyPath))) {
  throw "启用 TLS TURN 时必须提供 certPath 和 pkeyPath。"
}
if ($RemoteRoot -notmatch '^/[A-Za-z0-9_./-]+$') {
  throw "RemoteRoot 只能使用简单的 Linux 绝对路径，例如 /opt/lwylink。当前值：$RemoteRoot"
}

$turnURL = "turn:${PublicHost}:${ListeningPort}?transport=udp"
$turnsURL = ""
if ($EnableTLS) {
  $turnsURL = "turns:${PublicHost}:${TLSListeningPort}?transport=tcp"
}

if ($ValidateOnly) {
  Write-Host "TURN 配置预检通过：$turnURL" -ForegroundColor Green
  if ($turnsURL -ne "") { Write-Host "TLS TURN：$turnsURL" -ForegroundColor Green }
  return
}

if ($UpdateLocalControlConfig) {
  Invoke-Step "更新本机 control.prod.json TURN 配置" {
    Update-ControlConfig (Join-Path $repoRoot "configs\control.prod.json") $turnURL $turnsURL $StaticAuthSecret $TTLSeconds $UsernamePrefix
  }
  if ($LocalConfigOnly) {
    Write-Host "`n本机 control.prod.json TURN 配置已更新，未连接服务器。" -ForegroundColor Green
    return
  }
} elseif ($LocalConfigOnly) {
  throw "-LocalConfigOnly 需要同时指定 -UpdateLocalControlConfig。"
}

$client = New-DeployClient
Assert-DeployAuthReady $client

$tlsLines = ""
if ($EnableTLS) {
  $tlsLines = @"
tls-listening-port=$TLSListeningPort
cert=$CertPath
pkey=$PKeyPath
"@
} else {
  $tlsLines = @"
no-tls
no-dtls
"@
}

Invoke-Step "安装并配置 coturn" {
  Invoke-Remote $client @"
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if ! command -v turnserver >/dev/null 2>&1; then
  apt-get update
  apt-get install -y coturn
fi
install -d -m 0755 /etc/turnserver
cat >/etc/turnserver.conf <<'TURNCONF'
listening-port=$ListeningPort
fingerprint
use-auth-secret
static-auth-secret=$StaticAuthSecret
realm=$Realm
server-name=$Realm
no-cli
no-tlsv1
no-tlsv1_1
no-multicast-peers
no-loopback-peers
stale-nonce=600
total-quota=100
bps-capacity=0
min-port=$MinPort
max-port=$MaxPort
$tlsLines
TURNCONF
chmod 600 /etc/turnserver.conf
if [ -f /etc/default/coturn ]; then
  sed -i 's/^#\?TURNSERVER_ENABLED=.*/TURNSERVER_ENABLED=1/' /etc/default/coturn
fi
systemctl enable --now coturn
systemctl restart coturn
sleep 1
systemctl is-active coturn
ss -lntup | grep -E ':($ListeningPort|$TLSListeningPort) ' || true
"@ 240
}

if ($RestartControl) {
  Invoke-Step "重启控制中心以读取 TURN 配置" {
    Invoke-Remote $client @"
set -e
systemctl restart lwylink-control
sleep 1
systemctl is-active lwylink-control
"@ 60
  }
}

Write-Host "`nTURN 兜底配置完成：$turnURL" -ForegroundColor Green
if ($turnsURL -ne "") {
  Write-Host "TLS TURN：$turnsURL" -ForegroundColor Green
}
Write-Host "如已更新本机 control.prod.json，请运行 .\deploy\upload-deploy.ps1 -UploadConfigs -SkipSmoke 或下次 release 时带 -UploadConfigs。" -ForegroundColor Yellow

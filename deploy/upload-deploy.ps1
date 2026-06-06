param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [string]$RemoteRoot = "/opt/lwylink",
  [string]$DistDir = "",
  [switch]$UploadConfigs,
  [switch]$SkipSmoke,
  [string]$AdminToken = "",
  [string]$ApiURL = "",
  [string]$OverlayAddr = ""
)

$ErrorActionPreference = "Stop"

if ($HostName -eq "") { $HostName = $env:LWYLINK_DEPLOY_HOST }
if ($HostName -eq "") { $HostName = "64.83.34.53" }

if ($User -eq "") { $User = $env:LWYLINK_DEPLOY_SSH_USER }
if ($User -eq "") { $User = "root" }

if ($Port -eq 0 -and $env:LWYLINK_DEPLOY_SSH_PORT) { $Port = [int]$env:LWYLINK_DEPLOY_SSH_PORT }
if ($Port -eq 0) { $Port = 22 }

if ($Password -eq "") { $Password = $env:LWYLINK_DEPLOY_SSH_PASSWORD }

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
if ($DistDir -eq "") {
  $DistDir = Join-Path $repoRoot "deploy\dist"
} elseif (-not [System.IO.Path]::IsPathRooted($DistDir)) {
  $DistDir = Join-Path $repoRoot $DistDir
}

if ($RemoteRoot -notmatch '^/[A-Za-z0-9_./-]+$') {
  throw "RemoteRoot 只能使用简单的 Linux 绝对路径，例如 /opt/lwylink。当前值：$RemoteRoot"
}

if ($ApiURL -eq "") { $ApiURL = "http://${HostName}:8080" }
if ($OverlayAddr -eq "") { $OverlayAddr = "${HostName}:35768" }

if ($AdminToken -eq "") { $AdminToken = $env:LWYLINK_ADMIN_TOKEN }
if ($AdminToken -eq "") {
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    $AdminToken = ((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken
  }
}

function Invoke-Step {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
}

function Assert-File {
  param([string]$Path)
  if (-not (Test-Path $Path)) {
    throw "缺少部署产物：$Path。请先执行 .\deploy\build-all.ps1"
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

function Invoke-Remote {
  param(
    [object]$Client,
    [string]$Command,
    [int]$TimeOut = 90
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

function Send-File {
  param(
    [object]$Client,
    [string]$Path,
    [string]$Destination
  )

  if ($Client.mode -eq "posh") {
    if (-not $script:SFTPSession) {
      $script:SFTPSession = New-SFTPSession -ComputerName $HostName -Port $Port -Credential $Client.credential -AcceptKey
    }
    Set-SFTPItem -SessionId $script:SFTPSession.SessionId -Path $Path -Destination $Destination -Force
    return
  }

  $target = "${User}@${HostName}:$Destination"
  & scp -P $Port $Path $target
  if ($LASTEXITCODE -ne 0) {
    throw "上传失败：$Path -> $Destination"
  }
}

$controlBin = Join-Path $DistDir "lwylink-control"
$nodeBin = Join-Path $DistDir "lwylink-node"
$smokeBin = Join-Path $DistDir "lwylink-smoke"
$controlService = Join-Path $repoRoot "deploy\lwylink-control.service"
$nodeService = Join-Path $repoRoot "deploy\lwylink-node.service"

Assert-File $controlBin
Assert-File $nodeBin
Assert-File $smokeBin
Assert-File $controlService
Assert-File $nodeService
if ($UploadConfigs) {
  Assert-File (Join-Path $repoRoot "configs\control.prod.json")
  Assert-File (Join-Path $repoRoot "configs\node.prod.json")
}

Set-Location $repoRoot
$client = New-DeployClient

try {
  Invoke-Step "准备远端目录" {
    Invoke-Remote $client @"
set -e
mkdir -p $RemoteRoot/deploy/dist $RemoteRoot/configs
"@ 30
  }

  Invoke-Step "上传二进制和 systemd 文件" {
    Send-File $client $controlBin "$RemoteRoot/deploy/dist"
    Send-File $client $nodeBin "$RemoteRoot/deploy/dist"
    Send-File $client $smokeBin "$RemoteRoot/deploy/dist"
    Send-File $client $controlService "$RemoteRoot/deploy"
    Send-File $client $nodeService "$RemoteRoot/deploy"
    if ($UploadConfigs) {
      Send-File $client (Join-Path $repoRoot "configs\control.prod.json") "$RemoteRoot/configs"
      Send-File $client (Join-Path $repoRoot "configs\node.prod.json") "$RemoteRoot/configs"
    }
  }

  Invoke-Step "安装并重启 LwyLink 服务" {
    Invoke-Remote $client @"
set -e
install -m 0755 $RemoteRoot/deploy/dist/lwylink-control $RemoteRoot/lwylink-control
install -m 0755 $RemoteRoot/deploy/dist/lwylink-node $RemoteRoot/lwylink-node
install -m 0755 $RemoteRoot/deploy/dist/lwylink-smoke $RemoteRoot/lwylink-smoke
install -m 0644 $RemoteRoot/deploy/lwylink-control.service /etc/systemd/system/lwylink-control.service
install -m 0644 $RemoteRoot/deploy/lwylink-node.service /etc/systemd/system/lwylink-node.service
chmod 600 $RemoteRoot/configs/*.prod.json 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now lwylink-control
systemctl enable --now lwylink-node
systemctl restart lwylink-control
systemctl restart lwylink-node
sleep 2
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(8080|35768) ' || true
ss -ltnup | grep -E ':443 ' || true
"@ 90
  }
} finally {
  if ($script:SFTPSession) {
    Remove-SFTPSession -SessionId $script:SFTPSession.SessionId | Out-Null
    $script:SFTPSession = $null
  }
}

if (-not $SkipSmoke) {
  Invoke-Step "远端功能 Smoke 测试" {
    $smokeExe = Join-Path $repoRoot "build\windows-amd64\lwylink-smoke.exe"
    $smokeArgs = @("-api", $ApiURL, "-overlay", $OverlayAddr)
    if ($AdminToken -ne "") {
      $smokeArgs += @("-admin-token", $AdminToken)
    }

    if (Test-Path $smokeExe) {
      & $smokeExe @smokeArgs
    } else {
      go run .\cmd\lwylink-smoke @smokeArgs
    }
  }
}

Write-Host "`n部署完成：$ApiURL / $OverlayAddr" -ForegroundColor Green

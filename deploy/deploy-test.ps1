param(
  [string]$HostName = "",
  [string]$User = "",
  [int]$Port = 0,
  [string]$Password = "",
  [switch]$SkipBuild,
  [switch]$UploadConfigs,
  [switch]$BuildWindowsUI,
  [switch]$SkipSmoke,
  [string]$AdminToken = ""
)

$ErrorActionPreference = "Stop"

if ($HostName -eq "") {
  $HostName = $env:LWYLINK_TEST_HOST
}
if ($HostName -eq "") {
  $HostName = "64.83.34.53"
}
if ($User -eq "") {
  $User = $env:LWYLINK_TEST_SSH_USER
}
if ($User -eq "") {
  $User = "root"
}
if ($Port -eq 0 -and $env:LWYLINK_TEST_SSH_PORT) {
  $Port = [int]$env:LWYLINK_TEST_SSH_PORT
}
if ($Port -eq 0) {
  $Port = 22
}
if ($Password -eq "") {
  $Password = $env:LWYLINK_TEST_SSH_PASSWORD
}
if ($Password -eq "") {
  throw "请先设置 `$env:LWYLINK_TEST_SSH_PASSWORD，或通过 -Password 传入测试服务器 SSH 密码。"
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$distDir = Join-Path $repoRoot "deploy\dist"
$remoteRoot = "/opt/lwylink"
$legacyRemoteRoot = "/opt/lwyv-gamenet"
$apiURL = "http://${HostName}:8080"
$overlayAddr = "${HostName}:35768"

if ($AdminToken -eq "" -and $env:LWYLINK_ADMIN_TOKEN) {
  $AdminToken = $env:LWYLINK_ADMIN_TOKEN
}
if ($AdminToken -eq "") {
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    $AdminToken = ((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken
  }
}

function Invoke-LocalStep {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
}

function Invoke-Remote {
  param(
    [string]$Command,
    [int]$TimeOut = 60
  )
  $session = New-SSHSession -ComputerName $HostName -Port $Port -Credential $cred -AcceptKey
  try {
    $result = Invoke-SSHCommand -SessionId $session.SessionId -Command $Command -TimeOut $TimeOut
    if ($result.Output) {
      $result.Output | ForEach-Object { Write-Host $_ }
    }
    if ($result.Error) {
      $result.Error | ForEach-Object { Write-Host $_ -ForegroundColor Yellow }
    }
    if ($result.ExitStatus -ne 0) {
      throw "远端命令失败，ExitStatus=$($result.ExitStatus)"
    }
  } finally {
    Remove-SSHSession -SessionId $session.SessionId | Out-Null
  }
}

Set-Location $repoRoot
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force
Import-Module Posh-SSH

$securePassword = ConvertTo-SecureString $Password -AsPlainText -Force
$cred = New-Object System.Management.Automation.PSCredential($User, $securePassword)

if (-not $SkipBuild) {
  Invoke-LocalStep "Go 测试" {
    go test -p 1 ./...
  }

  if ($BuildWindowsUI) {
    Invoke-LocalStep "构建 Windows UI" {
      Push-Location (Join-Path $repoRoot "cmd\lwylink-windows-ui")
      try {
        wails build
      } finally {
        Pop-Location
      }
    }
  }

  Invoke-LocalStep "构建 Linux 服务端二进制" {
    New-Item -ItemType Directory -Force -Path $distDir | Out-Null
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    try {
      go build -o .\deploy\dist\lwylink-control .\cmd\lwylink-control
      go build -o .\deploy\dist\lwylink-node .\cmd\lwylink-node
      go build -o .\deploy\dist\lwylink-smoke .\cmd\lwylink-smoke
    } finally {
      Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED -ErrorAction SilentlyContinue
    }
  }
}

Invoke-LocalStep "准备远端目录" {
  $cmd = @"
set -e
mkdir -p $remoteRoot/deploy/dist $remoteRoot/configs
if [ -d $legacyRemoteRoot/configs ] && [ ! -f $remoteRoot/configs/control.prod.json ]; then
  cp -a $legacyRemoteRoot/configs/. $remoteRoot/configs/
fi
"@
  Invoke-Remote $cmd 30
}

Invoke-LocalStep "上传二进制和服务文件" {
  $sftp = New-SFTPSession -ComputerName $HostName -Port $Port -Credential $cred -AcceptKey
  try {
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "lwylink-control") -Destination "$remoteRoot/deploy/dist" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "lwylink-node") -Destination "$remoteRoot/deploy/dist" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "lwylink-smoke") -Destination "$remoteRoot/deploy/dist" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "deploy\lwylink-control.service") -Destination "$remoteRoot/deploy" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "deploy\lwylink-node.service") -Destination "$remoteRoot/deploy" -Force
    if ($UploadConfigs) {
      Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "configs\control.prod.json") -Destination "$remoteRoot/configs" -Force
      Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "configs\node.prod.json") -Destination "$remoteRoot/configs" -Force
    }
  } finally {
    Remove-SFTPSession -SessionId $sftp.SessionId | Out-Null
  }
}

Invoke-LocalStep "安装并重启 LwyLink 服务" {
  $cmd = @"
set -e
install -m 0755 $remoteRoot/deploy/dist/lwylink-control $remoteRoot/lwylink-control
install -m 0755 $remoteRoot/deploy/dist/lwylink-node $remoteRoot/lwylink-node
install -m 0755 $remoteRoot/deploy/dist/lwylink-smoke $remoteRoot/lwylink-smoke
install -m 0644 $remoteRoot/deploy/lwylink-control.service /etc/systemd/system/lwylink-control.service
install -m 0644 $remoteRoot/deploy/lwylink-node.service /etc/systemd/system/lwylink-node.service
chmod 600 $remoteRoot/configs/*.prod.json 2>/dev/null || true
systemctl daemon-reload
systemctl stop lwyv-gamenet-control lwyv-gamenet-node 2>/dev/null || true
systemctl disable lwyv-gamenet-control lwyv-gamenet-node 2>/dev/null || true
systemctl enable --now lwylink-control
systemctl enable --now lwylink-node
systemctl restart lwylink-control
systemctl restart lwylink-node
sleep 2
systemctl is-active lwylink-control
systemctl is-active lwylink-node
ss -ltnup | grep -E ':(443|8080|35768) ' || true
"@
  Invoke-Remote $cmd 90
}

if (-not $SkipSmoke) {
  Invoke-LocalStep "远端功能 Smoke 测试" {
    $smokeArgs = @(".\cmd\lwylink-smoke", "-api", $apiURL, "-overlay", $overlayAddr)
    if ($AdminToken -ne "") {
      $smokeArgs += @("-admin-token", $AdminToken)
    }
    go run @smokeArgs
  }
}

Write-Host "`n部署完成：$apiURL / $overlayAddr" -ForegroundColor Green

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
  $HostName = $env:LWYV_TEST_HOST
}
if ($HostName -eq "") {
  $HostName = "64.83.34.53"
}
if ($User -eq "") {
  $User = $env:LWYV_TEST_SSH_USER
}
if ($User -eq "") {
  $User = "root"
}
if ($Port -eq 0 -and $env:LWYV_TEST_SSH_PORT) {
  $Port = [int]$env:LWYV_TEST_SSH_PORT
}
if ($Port -eq 0) {
  $Port = 22
}
if ($Password -eq "") {
  $Password = $env:LWYV_TEST_SSH_PASSWORD
}
if ($Password -eq "") {
  throw "请先设置 `$env:LWYV_TEST_SSH_PASSWORD，或通过 -Password 传入测试服务器 SSH 密码。"
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$distDir = Join-Path $repoRoot "deploy\dist"
$remoteRoot = "/opt/lwyv-gamenet"
$apiURL = "http://${HostName}:8080"
$overlayAddr = "${HostName}:35768"

if ($AdminToken -eq "" -and $env:LWYV_GAMENET_ADMIN_TOKEN) {
  $AdminToken = $env:LWYV_GAMENET_ADMIN_TOKEN
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
      Push-Location (Join-Path $repoRoot "cmd\gamenet-windows-ui")
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
      go build -o .\deploy\dist\gamenet-control .\cmd\gamenet-control
      go build -o .\deploy\dist\gamenet-node .\cmd\gamenet-node
      go build -o .\deploy\dist\gamenet-smoke .\cmd\gamenet-smoke
    } finally {
      Remove-Item Env:\GOOS,Env:\GOARCH,Env:\CGO_ENABLED -ErrorAction SilentlyContinue
    }
  }
}

Invoke-LocalStep "准备远端目录" {
  Invoke-Remote "mkdir -p $remoteRoot/deploy/dist $remoteRoot/configs" 30
}

Invoke-LocalStep "上传二进制" {
  $sftp = New-SFTPSession -ComputerName $HostName -Port $Port -Credential $cred -AcceptKey
  try {
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "gamenet-control") -Destination "$remoteRoot/deploy/dist" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "gamenet-node") -Destination "$remoteRoot/deploy/dist" -Force
    Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $distDir "gamenet-smoke") -Destination "$remoteRoot/deploy/dist" -Force
    if ($UploadConfigs) {
      Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "configs\control.prod.json") -Destination "$remoteRoot/configs" -Force
      Set-SFTPItem -SessionId $sftp.SessionId -Path (Join-Path $repoRoot "configs\node.prod.json") -Destination "$remoteRoot/configs" -Force
    }
  } finally {
    Remove-SFTPSession -SessionId $sftp.SessionId | Out-Null
  }
}

Invoke-LocalStep "安装并重启 GameNet 服务" {
  $cmd = @"
set -e
install -m 0755 $remoteRoot/deploy/dist/gamenet-control $remoteRoot/gamenet-control
install -m 0755 $remoteRoot/deploy/dist/gamenet-node $remoteRoot/gamenet-node
install -m 0755 $remoteRoot/deploy/dist/gamenet-smoke $remoteRoot/gamenet-smoke
chmod 600 $remoteRoot/configs/*.prod.json 2>/dev/null || true
systemctl restart lwyv-gamenet-control
systemctl restart lwyv-gamenet-node
sleep 2
systemctl is-active lwyv-gamenet-control
systemctl is-active lwyv-gamenet-node
ss -ltnp | grep -E ':(443|8080|35768) ' || true
"@
  Invoke-Remote $cmd 90
}

if (-not $SkipSmoke) {
  Invoke-LocalStep "远端功能 Smoke 测试" {
    $smokeArgs = @(".\cmd\gamenet-smoke", "-api", $apiURL, "-overlay", $overlayAddr)
    if ($AdminToken -ne "") {
      $smokeArgs += @("-admin-token", $AdminToken)
    }
    go run @smokeArgs
  }
}

Write-Host "`n部署完成：$apiURL / $overlayAddr" -ForegroundColor Green

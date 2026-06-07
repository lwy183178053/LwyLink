param(
  [switch]$SkipTests,
  [switch]$Vet,
  [switch]$SkipWindowsUI,
  [switch]$NoInstallWails,
  [string]$AppVersion = "",
  [string]$DistDir = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
if ($DistDir -eq "") {
  $DistDir = Join-Path $repoRoot "deploy\dist"
} elseif (-not [System.IO.Path]::IsPathRooted($DistDir)) {
  $DistDir = Join-Path $repoRoot $DistDir
}

$buildRoot = Join-Path $repoRoot "build"
$windowsDistDir = Join-Path $buildRoot "windows-amd64"
$netExe = Join-Path $buildRoot "lwylink-net.exe"

function Invoke-Step {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
}

function Invoke-GoBuildFor {
  param(
    [string]$Title,
    [string]$GOOS,
    [string]$GOARCH,
    [string]$CGO,
    [string]$Output,
    [string]$Package
  )

  Invoke-Step $Title {
    $oldGOOS = $env:GOOS
    $oldGOARCH = $env:GOARCH
    $oldCGO = $env:CGO_ENABLED
    $env:GOOS = $GOOS
    $env:GOARCH = $GOARCH
    $env:CGO_ENABLED = $CGO
    try {
      go build -trimpath -o $Output $Package
    } finally {
      if ($null -eq $oldGOOS) { Remove-Item Env:\GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $oldGOOS }
      if ($null -eq $oldGOARCH) { Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $oldGOARCH }
      if ($null -eq $oldCGO) { Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $oldCGO }
    }
  }
}

function Find-Wails {
  $cmd = Get-Command wails -ErrorAction SilentlyContinue
  if ($cmd) {
    return $cmd.Source
  }

  $goBinWails = Join-Path $env:USERPROFILE "go\bin\wails.exe"
  if (Test-Path $goBinWails) {
    return $goBinWails
  }

  if ($NoInstallWails) {
    throw "wails 未在 PATH 中，也未找到 $goBinWails。请安装 Wails，或去掉 -NoInstallWails 让脚本自动安装。"
  }

  Invoke-Step "安装 Wails CLI" {
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
  }

  if (Test-Path $goBinWails) {
    return $goBinWails
  }
  throw "Wails CLI 安装后仍未找到：$goBinWails"
}

function Add-Artifact {
  param(
    [System.Collections.Generic.List[object]]$Artifacts,
    [string]$Name,
    [string]$Path
  )

  if (-not (Test-Path $Path)) {
    throw "构建产物不存在：$Path"
  }
  $item = Get-Item $Path
  $hash = (Get-FileHash -Algorithm SHA256 $Path).Hash.ToLowerInvariant()
  $Artifacts.Add([pscustomobject]@{
    name = $Name
    path = $item.FullName
    sizeBytes = $item.Length
    sha256 = $hash
  }) | Out-Null
}

function Remove-GeneratedBuildArtifacts {
  foreach ($path in @(
    (Join-Path $buildRoot "*.exe"),
    (Join-Path $buildRoot "wintun.dll"),
    (Join-Path $buildRoot "artifacts.json"),
    (Join-Path $windowsDistDir "*.exe")
  )) {
    Remove-Item -Path $path -Force -ErrorAction SilentlyContinue
  }
}

function Remove-EmptyWindowsBuildDir {
  if ((Test-Path $windowsDistDir) -and -not (Get-ChildItem -Path $windowsDistDir -Force -ErrorAction SilentlyContinue)) {
    Remove-Item -Path $windowsDistDir -Force -ErrorAction SilentlyContinue
  }
}

Set-Location $repoRoot
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
New-Item -ItemType Directory -Force -Path $buildRoot | Out-Null
Remove-GeneratedBuildArtifacts

if ($AppVersion -ne "") {
  Write-Host "提示：-AppVersion 只通过 ldflags 注入 main.AppVersion，不会修改 wails.json 或 package.json。" -ForegroundColor Yellow
}

if (-not $SkipTests) {
  Invoke-Step "Go 测试" {
    go test -p 1 ./...
  }
}

if ($Vet) {
  Invoke-Step "Go vet" {
    go vet -p 1 ./...
  }
}

Invoke-GoBuildFor "构建 Linux 控制中心" "linux" "amd64" "0" (Join-Path $DistDir "lwylink-control") ".\cmd\lwylink-control"
Invoke-GoBuildFor "构建 Linux 房间节点" "linux" "amd64" "0" (Join-Path $DistDir "lwylink-node") ".\cmd\lwylink-node"
Invoke-GoBuildFor "构建 Linux smoke 工具" "linux" "amd64" "0" (Join-Path $DistDir "lwylink-smoke") ".\cmd\lwylink-smoke"

Invoke-GoBuildFor "构建 Windows 虚拟组网引擎" "windows" "amd64" "0" $netExe ".\cmd\lwylink-net"

if (-not $SkipWindowsUI) {
  $wails = Find-Wails
  Invoke-Step "构建 Wails Windows UI" {
    Push-Location (Join-Path $repoRoot "cmd\lwylink-windows-ui")
    try {
      $args = @("build")
      if ($AppVersion -ne "") {
        $args += @("-ldflags", "-X main.AppVersion=$AppVersion")
      }
      & $wails @args
    } finally {
      Pop-Location
    }
  }

  $wailsExe = Join-Path $repoRoot "cmd\lwylink-windows-ui\build\bin\LwyLink.exe"
  if (Test-Path $wailsExe) {
    New-Item -ItemType Directory -Force -Path $windowsDistDir | Out-Null
    Copy-Item $wailsExe (Join-Path $windowsDistDir "LwyLink.exe") -Force
  }
}

$artifacts = [System.Collections.Generic.List[object]]::new()
Add-Artifact $artifacts "linux-amd64/lwylink-control" (Join-Path $DistDir "lwylink-control")
Add-Artifact $artifacts "linux-amd64/lwylink-node" (Join-Path $DistDir "lwylink-node")
Add-Artifact $artifacts "linux-amd64/lwylink-smoke" (Join-Path $DistDir "lwylink-smoke")
Add-Artifact $artifacts "windows-amd64/lwylink-net.exe" $netExe
if (-not $SkipWindowsUI) {
  Add-Artifact $artifacts "windows-amd64/LwyLink.exe" (Join-Path $windowsDistDir "LwyLink.exe")
}
Remove-EmptyWindowsBuildDir

$manifestPath = Join-Path $repoRoot "build\artifacts.json"
$artifacts | ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 $manifestPath

Write-Host "`n构建完成：" -ForegroundColor Green
$artifacts | Format-Table name,sizeBytes,sha256,path -AutoSize
Write-Host "产物清单：$manifestPath" -ForegroundColor Green

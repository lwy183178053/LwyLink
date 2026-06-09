param(
  [switch]$SkipFrontend,
  [switch]$SkipExamples,
  [switch]$SkipReleaseScript,
  [switch]$Race,
  [switch]$Vet
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")

function Invoke-Step {
  param(
    [string]$Title,
    [scriptblock]$Body
  )
  Write-Host "`n==> $Title" -ForegroundColor Cyan
  & $Body
}

function Invoke-Native {
  param(
    [string]$Label,
    [string]$Command,
    [string[]]$Arguments = @()
  )
  & $Command @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$Label 失败，退出码：$LASTEXITCODE"
  }
}

function Remove-ExampleGeneratedFiles {
  $examplesRoot = Resolve-Path (Join-Path $repoRoot "examples")
  Get-ChildItem -Path $examplesRoot -Recurse -Directory -Filter "__pycache__" -ErrorAction SilentlyContinue | ForEach-Object {
    if ($_.FullName.StartsWith($examplesRoot.Path, [System.StringComparison]::OrdinalIgnoreCase)) {
      Remove-Item -LiteralPath $_.FullName -Recurse -Force
    }
  }
  Remove-Item -Path (Join-Path $examplesRoot "java\*.class") -Force -ErrorAction SilentlyContinue
}

function Test-TrackedReleaseHygiene {
  $tracked = & git ls-files
  if ($LASTEXITCODE -ne 0) {
    throw "无法读取 Git 跟踪文件列表。"
  }

  $bad = @()
  foreach ($path in $tracked) {
    $normalized = $path -replace "\\", "/"
    if ($normalized -eq "internal/tun/assets/wintun-amd64.dll") {
      continue
    }
    if ($normalized -match "^(build|deploy/dist)/") {
      $bad += $path
      continue
    }
    if ($normalized -match "^configs/.*\.(local|prod)\.json$") {
      $bad += $path
      continue
    }
    if ($normalized -match "\.(exe|dll|class|log)$") {
      $bad += $path
    }
  }

  if ($bad.Count -gt 0) {
    $list = ($bad | Sort-Object | Select-Object -First 20) -join "`n"
    throw "发现不应提交的产物或本地配置：`n$list"
  }
}

Set-Location $repoRoot

Invoke-Step "Go 测试" {
  Invoke-Native "Go 测试" "go" @("test", "./...")
}

if ($Race) {
  Invoke-Step "Go race 测试" {
    Invoke-Native "Go race 测试" "go" @("test", "-race", "./internal/client", "./internal/overlay", "./cmd/lwylink-windows-ui")
  }
}

if ($Vet) {
  Invoke-Step "Go vet" {
    Invoke-Native "Go vet" "go" @("vet", "./...")
  }
}

if (-not $SkipFrontend) {
  Invoke-Step "Windows UI 前端构建" {
    Push-Location (Join-Path $repoRoot "cmd\lwylink-windows-ui\frontend")
    try {
      Invoke-Native "Windows UI 前端构建" "npm.cmd" @("run", "build")
    } finally {
      Pop-Location
    }
  }
}

if (-not $SkipExamples) {
  Invoke-Step "Python 示例自检" {
    try {
      $python = "python"
      if (Get-Command uv -ErrorAction SilentlyContinue) {
        Invoke-Native "Python 示例语法检查" "uv" @("run", "python", "-m", "py_compile", ".\examples\python\lwylink.py", ".\examples\python\lwylink_room_example.py", ".\examples\python\lwylink_net_example.py")
        Invoke-Native "Python 房间示例自测" "uv" @("run", "python", ".\examples\python\lwylink_room_example.py", "--self-test")
        $python = "uv"
      } else {
        Invoke-Native "Python 示例语法检查" $python @("-m", "py_compile", ".\examples\python\lwylink.py", ".\examples\python\lwylink_room_example.py", ".\examples\python\lwylink_net_example.py")
        Invoke-Native "Python 房间示例自测" $python @(".\examples\python\lwylink_room_example.py", "--self-test")
      }
      if (-not (Test-Path ".\build\lwylink-net.exe")) {
        throw "缺少 build\lwylink-net.exe，无法运行 Python 组网示例自测。请先执行 .\deploy\build-all.ps1。"
      }
      if ($python -eq "uv") {
        Invoke-Native "Python 组网示例自测" "uv" @("run", "python", ".\examples\python\lwylink_net_example.py", "--self-test", "--exe", ".\build\lwylink-net.exe")
      } else {
        Invoke-Native "Python 组网示例自测" $python @(".\examples\python\lwylink_net_example.py", "--self-test", "--exe", ".\build\lwylink-net.exe")
      }
    } finally {
      Remove-ExampleGeneratedFiles
    }
  }

  Invoke-Step "Java 示例自检" {
    try {
      Invoke-Native "Java 示例编译" "javac" @(".\examples\java\LwyLink.java", ".\examples\java\LwyLinkRoomExample.java", ".\examples\java\LwyLinkNetExample.java")
      Invoke-Native "Java 房间示例自测" "java" @("-cp", ".\examples\java", "LwyLinkRoomExample", "--self-test")
      if (-not (Test-Path ".\build\lwylink-net.exe")) {
        throw "缺少 build\lwylink-net.exe，无法运行 Java 组网示例自测。请先执行 .\deploy\build-all.ps1。"
      }
      Invoke-Native "Java 组网示例自测" "java" @("-cp", ".\examples\java", "LwyLinkNetExample", "--self-test", "--exe", ".\build\lwylink-net.exe")
    } finally {
      Remove-ExampleGeneratedFiles
    }
  }
}

if (-not $SkipReleaseScript) {
  Invoke-Step "发布脚本自测" {
    Invoke-Native "发布脚本自测" "PowerShell" @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ".\deploy\test-release.ps1")
  }
}

Invoke-Step "发布文件卫生检查" {
  Test-TrackedReleaseHygiene
}

Invoke-Step "Git 空白检查" {
  Invoke-Native "Git 空白检查" "git" @("diff", "--check")
}

Write-Host "`n项目自检通过。" -ForegroundColor Green

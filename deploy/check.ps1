param(
  [switch]$SkipFrontend,
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

if (-not $SkipReleaseScript) {
  Invoke-Step "发布脚本自测" {
    Invoke-Native "发布脚本自测" "PowerShell" @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ".\deploy\test-release.ps1")
  }
}

Invoke-Step "Git 空白检查" {
  Invoke-Native "Git 空白检查" "git" @("diff", "--check")
}

Write-Host "`n项目自检通过。" -ForegroundColor Green

param(
  [string]$ApiURL = "",
  [string]$AdminToken = "",
  [string]$OldVersion = "0.2.0",
  [string]$ExpectedVersion = "0.2.1",
  [string]$OldClientPath = "",
  [switch]$BuildOldClient,
  [switch]$LaunchOldClient,
  [switch]$ApplySelfUpdater,
  [switch]$PrepareTestLogin,
  [switch]$RestoreSavedLogin,
  [switch]$ClickInstallUpdate,
  [switch]$CloseExistingClient,
  [switch]$NoInstallWails
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

function Read-AdminToken {
  if (-not [string]::IsNullOrWhiteSpace($AdminToken)) { return $AdminToken }
  if (-not [string]::IsNullOrWhiteSpace($env:LWYLINK_ADMIN_TOKEN)) { return $env:LWYLINK_ADMIN_TOKEN }
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    return [string](((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken)
  }
  return ""
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

function Resolve-URL {
  param(
    [string]$BaseURL,
    [string]$Value
  )
  $uri = [System.Uri]::new($Value, [System.UriKind]::RelativeOrAbsolute)
  if ($uri.IsAbsoluteUri) {
    return $uri.AbsoluteUri
  }
  $base = [System.Uri]::new($BaseURL.TrimEnd("/") + "/")
  return ([System.Uri]::new($base, $Value)).AbsoluteUri
}

function Normalize-SHA256 {
  param([string]$Value)
  $sha = ([string]$Value).Trim().ToLowerInvariant()
  if ($sha -notmatch '^[0-9a-f]{64}$') {
    throw "SHA256 格式非法：$Value"
  }
  return $sha
}

function Find-Wails {
  $cmd = Get-Command wails -ErrorAction SilentlyContinue
  if ($cmd) { return $cmd.Source }
  $goBinWails = Join-Path $env:USERPROFILE "go\bin\wails.exe"
  if (Test-Path $goBinWails) { return $goBinWails }
  if ($NoInstallWails) {
    throw "wails 未在 PATH 中，也未找到 $goBinWails。"
  }
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
  if (Test-Path $goBinWails) { return $goBinWails }
  throw "Wails CLI 安装后仍未找到：$goBinWails"
}

function Build-OldClient {
  $wails = Find-Wails
  $uiDir = Join-Path $repoRoot "cmd\lwylink-windows-ui"
  $outDir = Join-Path $repoRoot ("build\client-upgrade-test\old-$OldVersion-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
  $out = Join-Path $outDir "LwyLink.exe"
  Stop-ProcessByExecutablePath $out
  Push-Location $uiDir
  try {
    & $wails build -ldflags "-X main.AppVersion=$OldVersion" | Out-Host
    if ($LASTEXITCODE -ne 0) {
      throw "Wails old client build failed"
    }
  } finally {
    Pop-Location
  }
  $built = Join-Path $uiDir "build\bin\LwyLink.exe"
  if (-not (Test-Path $built)) {
    throw "旧版客户端构建后未找到：$built"
  }
  New-Item -ItemType Directory -Force -Path $outDir | Out-Null
  Copy-Item -LiteralPath $built -Destination $out -Force
  $wintun = Join-Path $uiDir "build\bin\wintun.dll"
  if (Test-Path $wintun) {
    Copy-Item -LiteralPath $wintun -Destination (Join-Path $outDir "wintun.dll") -Force
  }
  return (Resolve-Path $out).Path
}

function Stop-ProcessByExecutablePath {
  param([string]$ExecutablePath)
  $processes = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
    Where-Object { $_.ExecutablePath -eq $ExecutablePath }
  foreach ($process in $processes) {
    Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
  }
}

function Stop-ExistingLwyLink {
  $processes = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
    Where-Object {
      $_.ExecutablePath -and
      [System.IO.Path]::GetFileNameWithoutExtension($_.ExecutablePath).StartsWith("LwyLink", [System.StringComparison]::OrdinalIgnoreCase)
    }
  foreach ($process in $processes) {
    Stop-Process -Id $process.ProcessId -Force -ErrorAction SilentlyContinue
  }
}

function Save-ScreenShot {
  param(
    [string]$Name,
    [System.Diagnostics.Process]$Process = $null
  )
  Add-Type -AssemblyName System.Windows.Forms
  Add-Type -AssemblyName System.Drawing
  Add-Type @"
using System;
using System.Runtime.InteropServices;
public class LwyLinkCaptureWin32 {
  [StructLayout(LayoutKind.Sequential)]
  public struct RECT {
    public int Left;
    public int Top;
    public int Right;
    public int Bottom;
  }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdcBlt, uint nFlags);
}
"@ -ErrorAction SilentlyContinue

  $bitmap = $null
  $graphics = $null
  if ($Process -and $Process.MainWindowHandle -ne 0) {
    $rect = New-Object LwyLinkCaptureWin32+RECT
    if ([LwyLinkCaptureWin32]::GetWindowRect($Process.MainWindowHandle, [ref]$rect)) {
      $width = [Math]::Max(1, $rect.Right - $rect.Left)
      $height = [Math]::Max(1, $rect.Bottom - $rect.Top)
      $bitmap = New-Object System.Drawing.Bitmap $width, $height
      $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
      $hdc = $graphics.GetHdc()
      $printed = [LwyLinkCaptureWin32]::PrintWindow($Process.MainWindowHandle, $hdc, 2)
      $graphics.ReleaseHdc($hdc)
      if (-not $printed) {
        $graphics.CopyFromScreen((New-Object System.Drawing.Point $rect.Left, $rect.Top), [System.Drawing.Point]::Empty, (New-Object System.Drawing.Size $width, $height))
      }
    } else {
      $bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
      $bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
      $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
      $graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
    }
  } else {
    $bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
  }
  $outDir = Join-Path $repoRoot "build\client-upgrade-test"
  New-Item -ItemType Directory -Force -Path $outDir | Out-Null
  $out = Join-Path $outDir $Name
  $bitmap.Save($out, [System.Drawing.Imaging.ImageFormat]::Png)
  $graphics.Dispose()
  $bitmap.Dispose()
  return (Resolve-Path $out).Path
}

function New-UpgradeDiagnosticsPaths {
  $outDir = Join-Path $repoRoot "build\client-upgrade-test"
  New-Item -ItemType Directory -Force -Path $outDir | Out-Null
  $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
  return [pscustomobject]@{
    Log = Join-Path $outDir "upgrade-client-$stamp.log"
    State = Join-Path $outDir "upgrade-client-state-$stamp.json"
  }
}

function Start-ProcessWithEnvironment {
  param(
    [string]$FilePath,
    [string]$WorkingDirectory,
    [hashtable]$Environment
  )
  $psi = [System.Diagnostics.ProcessStartInfo]::new()
  $psi.FileName = $FilePath
  $psi.WorkingDirectory = $WorkingDirectory
  $psi.UseShellExecute = $false
  foreach ($key in $Environment.Keys) {
    $psi.Environment[$key] = [string]$Environment[$key]
  }
  $process = [System.Diagnostics.Process]::new()
  $process.StartInfo = $psi
  if (-not $process.Start()) {
    throw "启动旧版客户端失败：$FilePath"
  }
  return $process
}

function Read-DiagnosticState {
  param([string]$Path)
  if (-not (Test-Path $Path)) { return $null }
  try {
    return Get-Content $Path -Raw | ConvertFrom-Json
  } catch {
    return $null
  }
}

function Wait-UpgradeDiagnosticState {
  param(
    [string]$StatePath,
    [string]$LogPath,
    [string]$ExpectedVersion,
    [System.Diagnostics.Process]$Process,
    [int]$TimeoutSeconds = 150
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  $sawMandatory = $false
  $sawDownloading = $false
  $sawReady = $false
  $lastState = $null
  do {
    if ($Process.HasExited) {
      break
    }
    $state = Read-DiagnosticState $StatePath
    if ($null -ne $state -and $null -ne $state.update) {
      $lastState = $state
      $update = $state.update
      if ([string]$update.latestVersion -eq $ExpectedVersion -and [bool]$update.mandatory) {
        $sawMandatory = $true
      }
      if ([int64]$update.downloadedBytes -gt 0) {
        $sawDownloading = $true
      }
      if ([string]$update.status -eq "ready" -and [int]$update.progress -eq 100) {
        $sawReady = $true
        break
      }
    }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)

  if ($Process.HasExited) {
    $tail = ""
    if (Test-Path $LogPath) {
      $tail = ((Get-Content $LogPath -Tail 20) -join "`n")
    }
    throw "旧版客户端启动后已退出，无法验证 GUI 强制更新弹窗。ExitCode=$($Process.ExitCode)`n诊断日志：$LogPath`n$tail"
  }
  if (-not $sawMandatory) {
    throw "旧版客户端未确认强制更新状态。state=$StatePath log=$LogPath"
  }
  if (-not $sawDownloading) {
    throw "旧版客户端未观察到下载进度推进。state=$StatePath log=$LogPath"
  }
  if (-not $sawReady) {
    $status = "-"
    $progress = "-"
    if ($lastState -and $lastState.update) {
      $status = [string]$lastState.update.status
      $progress = [string]$lastState.update.progress
    }
    throw "旧版客户端在 $TimeoutSeconds 秒内未完成更新包下载。lastStatus=$status progress=$progress state=$StatePath log=$LogPath"
  }
  return $lastState
}

function Bring-LwyLinkToFront {
  param([string]$ExecutablePath)
  Add-Type @"
using System;
using System.Runtime.InteropServices;
public class LwyLinkWin32 {
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
}
"@
  $deadline = (Get-Date).AddSeconds(12)
  do {
    $process = Get-Process -Name LwyLink -ErrorAction SilentlyContinue |
      Where-Object { $_.MainWindowHandle -ne 0 -and ([string]::IsNullOrWhiteSpace($ExecutablePath) -or $_.Path -eq $ExecutablePath) } |
      Sort-Object StartTime -Descending |
      Select-Object -First 1
    if ($process) {
      [LwyLinkWin32]::ShowWindow($process.MainWindowHandle, 9) | Out-Null
      [LwyLinkWin32]::SetForegroundWindow($process.MainWindowHandle) | Out-Null
      Start-Sleep -Milliseconds 800
      return $process
    }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  return $null
}

function Click-LwyLinkInstallButton {
  param([System.Diagnostics.Process]$Process)
  try {
    Add-Type -AssemblyName UIAutomationClient
    Add-Type -AssemblyName UIAutomationTypes
    $root = [System.Windows.Automation.AutomationElement]::FromHandle($Process.MainWindowHandle)
    if ($root) {
      $buttonCondition = New-Object System.Windows.Automation.PropertyCondition -ArgumentList @(
        [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
        [System.Windows.Automation.ControlType]::Button
      )
      $buttons = $root.FindAll([System.Windows.Automation.TreeScope]::Subtree, $buttonCondition)
      $buttonNames = @()
      $button = $null
      foreach ($candidate in $buttons) {
        $name = [string]$candidate.Current.Name
        if (-not [string]::IsNullOrWhiteSpace($name)) {
          $buttonNames += $name
        }
        if ($name -like "*立即重启更新*" -or $name -like "*等待下载完成*") {
          $button = $candidate
          break
        }
      }
      if ($button) {
        $bounds = $button.Current.BoundingRectangle
        if (-not $bounds.IsEmpty) {
          $x = [int]($bounds.Left + ($bounds.Width / 2))
          $y = [int]($bounds.Top + ($bounds.Height / 2))
          Write-Host "UI Automation 定位安装按钮：name=$($button.Current.Name) x=$x y=$y"
          Add-Type @"
using System;
using System.Runtime.InteropServices;
public class LwyLinkUiaClickWin32 {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo);
}
"@ -ErrorAction SilentlyContinue
          [LwyLinkUiaClickWin32]::SetForegroundWindow($Process.MainWindowHandle) | Out-Null
          Start-Sleep -Milliseconds 300
          [LwyLinkUiaClickWin32]::SetCursorPos($x, $y) | Out-Null
          Start-Sleep -Milliseconds 150
          [LwyLinkUiaClickWin32]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
          Start-Sleep -Milliseconds 80
          [LwyLinkUiaClickWin32]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
          return
        }
      }
      if ($buttonNames.Count -gt 0) {
        Write-Host "UI Automation 未找到安装按钮，当前按钮：$($buttonNames -join ', ')" -ForegroundColor Yellow
      }
    }
  } catch {
    Write-Host "UI Automation 点击失败，回退鼠标点击：$($_.Exception.Message)" -ForegroundColor Yellow
  }

  Add-Type @"
using System;
using System.Runtime.InteropServices;
public class LwyLinkMouseWin32 {
  [StructLayout(LayoutKind.Sequential)]
  public struct RECT {
    public int Left;
    public int Top;
    public int Right;
    public int Bottom;
  }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo);
}
"@ -ErrorAction SilentlyContinue
  if (-not $Process -or $Process.MainWindowHandle -eq 0) {
    throw "无法点击安装按钮：缺少 LwyLink 主窗口句柄"
  }
  $rect = New-Object LwyLinkMouseWin32+RECT
  if (-not [LwyLinkMouseWin32]::GetWindowRect($Process.MainWindowHandle, [ref]$rect)) {
    throw "无法读取 LwyLink 窗口位置"
  }
  $width = [Math]::Max(1, $rect.Right - $rect.Left)
  $height = [Math]::Max(1, $rect.Bottom - $rect.Top)
  [LwyLinkMouseWin32]::ShowWindow($Process.MainWindowHandle, 9) | Out-Null
  [LwyLinkMouseWin32]::SetForegroundWindow($Process.MainWindowHandle) | Out-Null
  Start-Sleep -Milliseconds 500
  $x = [int]($rect.Left + ($width * 0.808))
  $y = [int]($rect.Top + ($height * 0.782))
  [LwyLinkMouseWin32]::SetCursorPos($x, $y) | Out-Null
  Start-Sleep -Milliseconds 150
  [LwyLinkMouseWin32]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
  Start-Sleep -Milliseconds 80
  [LwyLinkMouseWin32]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
  Start-Sleep -Milliseconds 250
  [LwyLinkMouseWin32]::mouse_event(0x0002, 0, 0, 0, [UIntPtr]::Zero)
  Start-Sleep -Milliseconds 80
  [LwyLinkMouseWin32]::mouse_event(0x0004, 0, 0, 0, [UIntPtr]::Zero)
}

function Wait-UpdateInstallStarted {
  param(
    [string]$StatePath,
    [System.Diagnostics.Process]$Process,
    [int]$TimeoutSeconds = 20
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  do {
    if ($Process.HasExited) {
      return "exited"
    }
    $state = Read-DiagnosticState $StatePath
    if ($state -and $state.update -and [string]$state.update.status -eq "installing") {
      return "installing"
    }
    Start-Sleep -Milliseconds 300
  } while ((Get-Date) -lt $deadline)
  return "timeout"
}

function Wait-ExecutableSHA256 {
  param(
    [string]$Path,
    [string]$ExpectedSHA256,
    [int]$TimeoutSeconds = 120
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  $last = ""
  do {
    try {
      if (Test-Path $Path) {
        $last = (Get-FileHash -Path $Path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($last -eq $ExpectedSHA256) {
          return $last
        }
      }
    } catch {
      $last = $_.Exception.Message
    }
    Start-Sleep -Milliseconds 500
  } while ((Get-Date) -lt $deadline)
  throw "等待客户端可执行文件替换超时：path=$Path last=$last want=$ExpectedSHA256"
}

function Get-CredentialsPath {
  $root = [Environment]::GetFolderPath([System.Environment+SpecialFolder]::ApplicationData)
  if ([string]::IsNullOrWhiteSpace($root)) {
    throw "无法定位 AppData Roaming 目录。"
  }
  return (Join-Path $root "LwyLink\credentials.json")
}

function Get-UpgradeLoginMetaPath {
  $root = [Environment]::GetFolderPath([System.Environment+SpecialFolder]::ApplicationData)
  return (Join-Path $root "LwyLink\upgrade-test-login.json")
}

function Protect-PasswordForClient {
  param([string]$Password)
  Add-Type -AssemblyName System.Security
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($Password)
  $protected = [System.Security.Cryptography.ProtectedData]::Protect(
    $bytes,
    $null,
    [System.Security.Cryptography.DataProtectionScope]::CurrentUser
  )
  return [Convert]::ToBase64String($protected)
}

function Write-JsonNoBom {
  param(
    [string]$Path,
    [object]$Value
  )
  $json = $Value | ConvertTo-Json -Depth 4
  $encoding = [System.Text.UTF8Encoding]::new($false)
  [System.IO.File]::WriteAllText($Path, $json, $encoding)
}

function Invoke-JSONRequest {
  param(
    [string]$Method,
    [string]$URL,
    [object]$Body = $null
  )
  $params = @{
    Method = $Method
    Uri = $URL
    TimeoutSec = 20
  }
  if ($null -ne $Body) {
    $params.ContentType = "application/json"
    $params.Body = ($Body | ConvertTo-Json -Depth 4 -Compress)
  }
  return Invoke-RestMethod @params
}

function Register-TestLogin {
  param(
    [string]$BaseURL,
    [string]$Token
  )
  if ([string]::IsNullOrWhiteSpace($Token)) {
    throw "准备测试登录态需要 AdminToken，用于 /api/smoke/register。"
  }
  $suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
  $email = "upgrade$suffix@smoke.lwylink.test"
  $password = "upgrade-test-password-$suffix"
  $displayName = "Upgrade Test"
  $result = Invoke-JSONRequest "Post" "$BaseURL/api/smoke/register?token=$([System.Uri]::EscapeDataString($Token))" @{
    email = $email
    password = $password
    displayName = $displayName
  }
  if (-not $result.user -or -not $result.user.id) {
    throw "测试账号注册结果缺少 user.id"
  }
  return [pscustomobject]@{
    Email = $email
    Password = $password
    UserID = [int64]$result.user.id
  }
}

function Backup-AndWriteSavedLogin {
  param(
    [string]$BaseURL,
    [object]$Login
  )
  $credentialsPath = Get-CredentialsPath
  $metaPath = Get-UpgradeLoginMetaPath
  $dir = Split-Path -Parent $credentialsPath
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $backupPath = ""
  if (Test-Path $credentialsPath) {
    $backupPath = Join-Path $dir ("credentials.before-upgrade-test." + [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() + ".json")
    Copy-Item -LiteralPath $credentialsPath -Destination $backupPath -Force
  }
  $protectedPassword = Protect-PasswordForClient $Login.Password
  Write-JsonNoBom $credentialsPath @{
    version = 2
    apiBaseURL = $BaseURL
    email = $Login.Email
    protectedPassword = $protectedPassword
  }
  Write-JsonNoBom $metaPath @{
    createdAt = (Get-Date).ToString("s")
    apiURL = $BaseURL
    email = $Login.Email
    userID = $Login.UserID
    backupPath = $backupPath
    credentialsPath = $credentialsPath
  }
  return [pscustomobject]@{
    CredentialsPath = $credentialsPath
    MetaPath = $metaPath
    BackupPath = $backupPath
  }
}

function Restore-SavedLogin {
  param(
    [string]$BaseURL,
    [string]$Token
  )
  $credentialsPath = Get-CredentialsPath
  $metaPath = Get-UpgradeLoginMetaPath
  if (-not (Test-Path $metaPath)) {
    Write-Host "未找到升级测试登录态元数据，跳过恢复：$metaPath" -ForegroundColor Yellow
    return
  }
  $meta = Get-Content $metaPath -Raw | ConvertFrom-Json
  if ($meta.backupPath -and (Test-Path ([string]$meta.backupPath))) {
    Copy-Item -LiteralPath ([string]$meta.backupPath) -Destination $credentialsPath -Force
    Remove-Item -LiteralPath ([string]$meta.backupPath) -Force -ErrorAction SilentlyContinue
  } else {
    Remove-Item -LiteralPath $credentialsPath -Force -ErrorAction SilentlyContinue
  }
  if ($meta.userID -and -not [string]::IsNullOrWhiteSpace($Token)) {
    $form = @{
      token = $Token
      userID = [string]$meta.userID
    }
    try {
      Invoke-WebRequest -Method Post -Uri "$BaseURL/admin/users/delete" -Body $form -UseBasicParsing -TimeoutSec 20 | Out-Null
    } catch {
      Write-Host "测试账号清理失败，可稍后手动删除 userID=$($meta.userID)：$($_.Exception.Message)" -ForegroundColor Yellow
    }
  }
  Remove-Item -LiteralPath $metaPath -Force -ErrorAction SilentlyContinue
  Write-Host "已恢复客户端登录态：$credentialsPath"
}

$ApiURL = (Get-DeployConfigValue $ApiURL "LWYLINK_API_URL" "apiURL" "https://www.lwylink.xyz").TrimEnd("/")
$token = Read-AdminToken

if ($RestoreSavedLogin) {
  Restore-SavedLogin $ApiURL $token
  return
}

Write-Host "==> 读取线上 latest release" -ForegroundColor Cyan
$latest = Invoke-RestMethod -Uri "$ApiURL/api/client/releases/latest?platform=windows-amd64" -Method Get -TimeoutSec 20
if ([string]::IsNullOrWhiteSpace([string]$latest.version) -or [string]::IsNullOrWhiteSpace([string]$latest.downloadURL)) {
  throw "latest release 缺少 version 或 downloadURL"
}
if ($ExpectedVersion -ne "" -and [string]$latest.version -ne $ExpectedVersion) {
  throw "latest release version mismatch: got $($latest.version), want $ExpectedVersion"
}
if (-not [bool]$latest.mandatory) {
  throw "latest release 不是强制更新，无法验证强制更新体验"
}
$sha = Normalize-SHA256 ([string]$latest.sha256)
Write-Host "latest=$($latest.version) mandatory=$($latest.mandatory) size=$($latest.sizeBytes) sha=$sha"

Write-Host "==> 下载真实 latest 客户端包并校验 SHA" -ForegroundColor Cyan
$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("LwyLinkUpgradeTest-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null
$downloadPath = Join-Path $tmpRoot "latest-LwyLink.exe"
$downloadURL = Add-QueryParameter (Resolve-URL $ApiURL ([string]$latest.downloadURL)) "token" $token
Invoke-WebRequest -Uri $downloadURL -UseBasicParsing -TimeoutSec 180 -OutFile $downloadPath
$actualSHA = (Get-FileHash -Path $downloadPath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA -ne $sha) {
  throw "latest 下载 SHA256 不匹配：got $actualSHA, want $sha"
}
Write-Host "download ok path=$downloadPath"

if ($BuildOldClient) {
  Write-Host "==> 构建旧版客户端 $OldVersion" -ForegroundColor Cyan
  $OldClientPath = Build-OldClient
  Write-Host "old client=$OldClientPath"
}
if ([string]::IsNullOrWhiteSpace($OldClientPath)) {
  $candidate = Join-Path $repoRoot "build\client-upgrade-test\LwyLink-$OldVersion.exe"
  if (Test-Path $candidate) {
    $OldClientPath = (Resolve-Path $candidate).Path
  }
}
if (-not [string]::IsNullOrWhiteSpace($OldClientPath) -and -not (Test-Path $OldClientPath)) {
  throw "旧版客户端不存在：$OldClientPath"
}

if ($PrepareTestLogin) {
  Write-Host "==> 准备旧版客户端自动登录测试账号" -ForegroundColor Cyan
  $login = Register-TestLogin $ApiURL $token
  $saved = Backup-AndWriteSavedLogin $ApiURL $login
  Write-Host "test login email=$($login.Email) userID=$($login.UserID)"
  Write-Host "credentials=$($saved.CredentialsPath)"
  if ($saved.BackupPath) {
    Write-Host "原登录态已备份：$($saved.BackupPath)"
  }
  Write-Host "恢复命令：PowerShell -NoProfile -ExecutionPolicy Bypass -File .\deploy\test-client-upgrade.ps1 -RestoreSavedLogin"
}

if ($ApplySelfUpdater) {
  if ([string]::IsNullOrWhiteSpace($OldClientPath)) {
    throw "执行自更新替换需要 -OldClientPath 或 -BuildOldClient"
  }
  Write-Host "==> 使用旧版客户端 updater 参数验证替换和重启" -ForegroundColor Cyan
  $sandbox = Join-Path $tmpRoot "sandbox"
  New-Item -ItemType Directory -Force -Path $sandbox | Out-Null
  $target = Join-Path $sandbox "LwyLink.exe"
  Copy-Item -LiteralPath $OldClientPath -Destination $target -Force
  $proc = Start-Process -FilePath $target -ArgumentList @("--lwylink-apply-update", $downloadPath, $target) -PassThru
  if (-not $proc.WaitForExit(90000)) {
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    throw "updater 90 秒内未退出"
  }
  if ($proc.ExitCode -ne 0) {
    throw "updater 退出码异常：$($proc.ExitCode)"
  }
  $targetSHA = (Get-FileHash -Path $target -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($targetSHA -ne $sha) {
    throw "替换后的目标文件 SHA256 不匹配：got $targetSHA, want $sha"
  }
  Start-Sleep -Seconds 2
  Stop-ProcessByExecutablePath $target
  Write-Host "self updater ok target=$target"
}

if ($LaunchOldClient) {
  if ([string]::IsNullOrWhiteSpace($OldClientPath)) {
    throw "启动旧版客户端需要 -OldClientPath 或 -BuildOldClient"
  }
  if ($CloseExistingClient) {
    Stop-ExistingLwyLink
    Start-Sleep -Seconds 1
  }
  Write-Host "==> 启动旧版客户端，请登录后观察强制更新弹窗、进度条、下载完成和重启更新按钮" -ForegroundColor Cyan
  $diag = New-UpgradeDiagnosticsPaths
  $launchEnv = @{
    LWYLINK_UPGRADE_TEST_LOG = $diag.Log
    LWYLINK_UPGRADE_TEST_STATE = $diag.State
  }
  if ($ClickInstallUpdate) {
    $launchEnv["LWYLINK_UPGRADE_TEST_AUTO_INSTALL"] = "1"
  }
  $launched = Start-ProcessWithEnvironment -FilePath $OldClientPath -WorkingDirectory (Split-Path -Parent $OldClientPath) -Environment $launchEnv
  Write-Host "诊断日志：$($diag.Log)"
  Write-Host "状态快照：$($diag.State)"
  $verifiedState = Wait-UpgradeDiagnosticState -StatePath $diag.State -LogPath $diag.Log -ExpectedVersion $ExpectedVersion -Process $launched
  Write-Host "强制更新状态已验证：latest=$($verifiedState.update.latestVersion) status=$($verifiedState.update.status) progress=$($verifiedState.update.progress)%"
  $frontProcess = Bring-LwyLinkToFront $OldClientPath
  if (-not $frontProcess) {
    Write-Host "旧版客户端状态链路已通过，但没有可截图的主窗口。pid=$($launched.Id)" -ForegroundColor Yellow
    return
  }
  Write-Host "LwyLink 窗口：pid=$($frontProcess.Id) path=$($frontProcess.Path)"
  $shot = Save-ScreenShot ("upgrade-gui-" + (Get-Date -Format "yyyyMMdd-HHmmss") + ".png") $frontProcess
  Write-Host "GUI 截图：$shot"
  if ($ClickInstallUpdate) {
    Write-Host "==> 触发强制更新安装，并等待可执行文件替换" -ForegroundColor Cyan
    Click-LwyLinkInstallButton $frontProcess
    $installSignal = Wait-UpdateInstallStarted -StatePath $diag.State -Process $launched
    if ($installSignal -eq "timeout") {
      $tail = ""
      if (Test-Path $diag.Log) {
        $tail = ((Get-Content $diag.Log -Tail 30) -join "`n")
      }
      throw "点击后未观察到安装启动。state=$($diag.State) log=$($diag.Log)`n$tail"
    }
    $launched.WaitForExit(15000) | Out-Null
    $newSHA = Wait-ExecutableSHA256 -Path $OldClientPath -ExpectedSHA256 $sha
    Write-Host "GUI 安装替换完成：path=$OldClientPath sha=$newSHA"
    Stop-ProcessByExecutablePath $OldClientPath
  }
}

Write-Host "`n客户端升级体验检查完成。" -ForegroundColor Green
Write-Host "可复现实测命令：" -ForegroundColor Green
Write-Host "  PowerShell -NoProfile -ExecutionPolicy Bypass -File .\deploy\test-client-upgrade.ps1 -BuildOldClient -ApplySelfUpdater"
Write-Host "GUI 观察命令：" -ForegroundColor Green
Write-Host "  PowerShell -NoProfile -ExecutionPolicy Bypass -File .\deploy\test-client-upgrade.ps1 -BuildOldClient -PrepareTestLogin -LaunchOldClient -CloseExistingClient"
Write-Host "GUI 全链路命令：" -ForegroundColor Green
Write-Host "  PowerShell -NoProfile -ExecutionPolicy Bypass -File .\deploy\test-client-upgrade.ps1 -BuildOldClient -PrepareTestLogin -LaunchOldClient -ClickInstallUpdate -CloseExistingClient"

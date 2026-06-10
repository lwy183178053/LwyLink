param(
  [ValidateSet("list", "clear", "keep-latest")]
  [string]$Action = "list",
  [string]$ApiURL = "",
  [string]$AdminToken = "",
  [string]$Platform = "windows-amd64",
  [int]$Keep = 1,
  [switch]$AllPlatforms,
  [switch]$DryRun
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

function Read-AdminToken {
  if (-not [string]::IsNullOrWhiteSpace($AdminToken)) { return $AdminToken }
  if (-not [string]::IsNullOrWhiteSpace($env:LWYLINK_ADMIN_TOKEN)) { return $env:LWYLINK_ADMIN_TOKEN }
  $controlProdConfig = Join-Path $repoRoot "configs\control.prod.json"
  if (Test-Path $controlProdConfig) {
    return [string](((Get-Content $controlProdConfig -Raw) | ConvertFrom-Json).adminToken)
  }
  return ""
}

function Strip-Html {
  param([string]$Value)
  $text = [regex]::Replace([string]$Value, '<[^>]+>', '')
  return [System.Net.WebUtility]::HtmlDecode($text).Trim()
}

function Get-VersionSortKey {
  param([string]$Version)
  $value = ([string]$Version).Trim().TrimStart("v", "V")
  $main = $value
  $pre = ""
  $dash = $value.IndexOf("-")
  if ($dash -ge 0) {
    $main = $value.Substring(0, $dash)
    $pre = $value.Substring($dash + 1).ToLowerInvariant()
  }
  $parts = @($main.Split(".") | ForEach-Object {
    $n = 0
    [void][int]::TryParse($_, [ref]$n)
    "{0:D10}" -f $n
  })
  while ($parts.Count -lt 4) {
    $parts += "0000000000"
  }
  $stable = "1"
  if ($pre -ne "") { $stable = "0" }
  return ($parts -join ".") + ".$stable.$pre"
}

function Get-ClientReleases {
  param(
    [string]$BaseURL,
    [string]$Token
  )
  $url = Add-QueryParameter "$BaseURL/admin/releases?perPage=1000" "token" $Token
  $html = (Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 30).Content
  $items = @()
  foreach ($match in [regex]::Matches($html, '(?s)<tr>\s*<td class="mono">(\d+)</td>.*?</tr>')) {
    $row = $match.Value
    $releaseIDMatch = [regex]::Match($row, 'name="releaseID"\s+value="(\d+)"')
    if (-not $releaseIDMatch.Success) { continue }
    $cells = @([regex]::Matches($row, '(?s)<td[^>]*>(.*?)</td>') | ForEach-Object { Strip-Html $_.Groups[1].Value })
    if ($cells.Count -lt 10) { continue }
    $rowID = [int64]$cells[0]
    $formID = [int64]$releaseIDMatch.Groups[1].Value
    if ($rowID -ne $formID) {
      throw "release id mismatch: row=$rowID form=$formID"
    }
    $items += [pscustomobject]@{
      ID         = $rowID
      Version    = $cells[1]
      Platform   = $cells[2]
      File       = $cells[3]
      Size       = $cells[4]
      Mandatory  = $cells[5]
      SHA256     = $cells[7]
      Notes      = $cells[8]
      UploadedAt = $cells[9]
      SortKey    = Get-VersionSortKey $cells[1]
    }
  }
  return $items
}

function Remove-ClientRelease {
  param(
    [string]$BaseURL,
    [string]$Token,
    [object]$Release
  )
  if ($DryRun) {
    Write-Host "DRY-RUN delete id=$($Release.ID) version=$($Release.Version) platform=$($Release.Platform)"
    return
  }
  Invoke-WebRequest -Method Post -Uri "$BaseURL/admin/releases/delete" -Body @{
    token = $Token
    releaseID = [string]$Release.ID
  } -UseBasicParsing -TimeoutSec 30 | Out-Null
  Write-Host "deleted id=$($Release.ID) version=$($Release.Version) platform=$($Release.Platform)"
}

$ApiURL = Get-DeployConfigValue $ApiURL "LWYLINK_API_URL" "apiURL" "https://www.lwylink.xyz"
$ApiURL = $ApiURL.TrimEnd("/")
$token = Read-AdminToken
if ([string]::IsNullOrWhiteSpace($token)) {
  throw "缺少 admin token。请设置 -AdminToken 或 LWYLINK_ADMIN_TOKEN，或提供 configs\control.prod.json。"
}
if ($Keep -lt 0) {
  throw "Keep 不能小于 0。"
}

$releases = @(Get-ClientReleases $ApiURL $token)
if (-not $AllPlatforms) {
  $releases = @($releases | Where-Object { $_.Platform -eq $Platform })
}

switch ($Action) {
  "list" {
    if ($releases.Count -eq 0) {
      Write-Host "暂无客户端发布记录。"
      return
    }
    $releases |
      Sort-Object Platform, @{ Expression = "SortKey"; Descending = $true }, @{ Expression = "ID"; Descending = $true } |
      Select-Object ID, Version, Platform, File, Size, Mandatory, UploadedAt |
      Format-Table -AutoSize
  }
  "clear" {
    if ($releases.Count -eq 0) {
      Write-Host "没有需要删除的客户端发布记录。"
      return
    }
    foreach ($release in ($releases | Sort-Object ID -Descending)) {
      Remove-ClientRelease $ApiURL $token $release
    }
  }
  "keep-latest" {
    if ($Keep -lt 1) {
      throw "keep-latest 的 Keep 必须大于 0；如需全删请使用 -Action clear。"
    }
    $delete = @()
    foreach ($group in ($releases | Group-Object Platform)) {
      $sorted = @($group.Group | Sort-Object @{ Expression = "SortKey"; Descending = $true }, @{ Expression = "ID"; Descending = $true })
      $delete += @($sorted | Select-Object -Skip $Keep)
    }
    if ($delete.Count -eq 0) {
      Write-Host "无需删除，当前记录数量未超过保留数量。"
      return
    }
    foreach ($release in ($delete | Sort-Object ID -Descending)) {
      Remove-ClientRelease $ApiURL $token $release
    }
  }
}

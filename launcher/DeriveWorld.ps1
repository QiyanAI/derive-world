# SPDX-License-Identifier: Apache-2.0
<#
.SYNOPSIS
  Derive World local launcher for Windows.

.DESCRIPTION
  A small, dependency-free helper for a self-hosting administrator on Windows.
  It checks the local prerequisites, starts/stops the Compose stack, opens the
  product, and opens/applies the AI model configuration in this instance's
  .env file.

  It is not a service, opens no network listener, never prints the API key,
  never passes secrets on a command line, and never deletes the database
  volume. It does not install software.

  Run with no arguments for the interactive menu, or use -Command for
  automation:

    powershell -ExecutionPolicy Bypass -File .\launcher\DeriveWorld.ps1 -Command Check
    powershell -ExecutionPolicy Bypass -File .\launcher\DeriveWorld.ps1 -Command Start
    powershell -ExecutionPolicy Bypass -File .\launcher\DeriveWorld.ps1 -Command SelfTest -NoPause

.PARAMETER Command
  Menu, Start, Stop, Restart, Status, Check, Diagnostics, OpenConfig,
  ApplyConfig, Open, SelfTest.

.PARAMETER NoBrowser
  Do not open the product URL after Start.

.PARAMETER NoPause
  Do not wait for a key press before exiting (for automation/CI).

.PARAMETER AllowMissingDocker
  Environment check exits successfully even when Docker is absent (CI).

.PARAMETER Force
  Apply AI settings without interactive confirmation prompts.

.PARAMETER HealthTimeoutSeconds
  How long to wait for /readyz after start/restart (default 180).
#>
[CmdletBinding()]
param(
    [ValidateSet('Menu', 'Start', 'Stop', 'Restart', 'Status', 'Check', 'Diagnostics',
        'OpenConfig', 'ApplyConfig', 'StatusConfig', 'UseFixture', 'Open', 'SelfTest')]
    [string]$Command = 'Menu',

    [switch]$NoBrowser,
    [switch]$NoPause,
    [switch]$AllowMissingDocker,
    [switch]$Force,
    [int]$HealthTimeoutSeconds = 180
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:Root = Split-Path -Parent $PSScriptRoot
$script:EnvPath = Join-Path $script:Root '.env'
$script:EnvExamplePath = Join-Path $script:Root '.env.example'
$script:LastGoodPath = Join-Path $script:Root '.env.last-good'
$script:ComposePath = Join-Path $script:Root 'compose.yaml'
$script:AppUrl = 'http://127.0.0.1:8080'
$script:AppPort = 8080
$script:IsWindowsHost = $true
if ($PSVersionTable.PSVersion.Major -ge 6) {
    $script:IsWindowsHost = $IsWindows
}

function Write-Title {
    param([string]$Text)
    Write-Host ''
    Write-Host ('=' * 68) -ForegroundColor DarkCyan
    Write-Host $Text -ForegroundColor Cyan
    Write-Host ('=' * 68) -ForegroundColor DarkCyan
}

function Write-Info {
    param([string]$Text)
    Write-Host $Text -ForegroundColor Gray
}

function Write-Ok {
    param([string]$Text)
    Write-Host ("[OK]   " + $Text) -ForegroundColor Green
}

function Write-Warn {
    param([string]$Text)
    Write-Host ("[WARN] " + $Text) -ForegroundColor Yellow
}

function Write-Fail {
    param([string]$Text)
    Write-Host ("[FAIL] " + $Text) -ForegroundColor Red
}

function Write-Step {
    param([string]$Text)
    Write-Host ("[..]   " + $Text) -ForegroundColor Cyan
}

function Read-EnvFile {
    param([string]$Path)
    $map = @{}
    if (-not (Test-Path -LiteralPath $Path)) {
        return $map
    }
    foreach ($line in (Get-Content -LiteralPath $Path -Encoding UTF8)) {
        $trimmed = $line.Trim()
        if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#')) {
            continue
        }
        $index = $trimmed.IndexOf('=')
        if ($index -lt 1) {
            continue
        }
        $key = $trimmed.Substring(0, $index).Trim()
        $value = $trimmed.Substring($index + 1).Trim()
        if ($value.Length -ge 2) {
            $first = $value.Substring(0, 1)
            $last = $value.Substring($value.Length - 1, 1)
            if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
                $value = $value.Substring(1, $value.Length - 2)
            }
        }
        $map[$key] = $value
    }
    return $map
}

function Get-EnvValue {
    param([hashtable]$Map, [string]$Key)
    if ($Map.ContainsKey($Key)) {
        return [string]$Map[$Key]
    }
    return ''
}

function Get-MaskedValue {
    param([string]$Value)
    if ([string]::IsNullOrEmpty($Value)) {
        return '(empty)'
    }
    return '(set, hidden)'
}

function Protect-ConfigFile {
    param([string]$Path)
    if (-not $script:IsWindowsHost) {
        Write-Warn 'File permissions are managed by the host operating system.'
        return
    }
    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $icacls = Get-Command icacls.exe -ErrorAction SilentlyContinue
    if (-not $icacls) {
        Write-Warn 'icacls.exe was not found; restrict .env permissions manually.'
        return
    }
    try {
        $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
        $aclOutput = & icacls.exe $Path /inheritance:r /grant:r "${identity}:(R,W)" '*S-1-5-18:(R,W)' '*S-1-5-32-544:(R,W)' 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Warn ('Could not restrict .env permissions automatically: ' + (($aclOutput | Out-String).Trim()))
            return
        }
        Write-Ok 'Restricted .env permissions to the current user, SYSTEM, and Administrators.'
    }
    catch {
        Write-Warn ('Could not restrict .env permissions automatically: ' + $_.Exception.Message)
    }
}

function Ensure-EnvFile {
    if (Test-Path -LiteralPath $script:EnvPath) {
        return $true
    }
    if (-not (Test-Path -LiteralPath $script:EnvExamplePath)) {
        Write-Fail '.env is missing and .env.example is not available. Restore the repository files first.'
        return $false
    }
    Copy-Item -LiteralPath $script:EnvExamplePath -Destination $script:EnvPath
    Protect-ConfigFile -Path $script:EnvPath
    Write-Ok 'Created .env from .env.example (Fixture mode). Existing files were not overwritten.'
    return $true
}

function Save-LastGoodConfig {
    if (-not (Test-Path -LiteralPath $script:EnvPath)) {
        return $false
    }
    $map = Read-EnvFile -Path $script:EnvPath
    if (@(Test-RequiredConfig -Map $map).Count -ne 0) {
        Write-Warn 'The current .env is not valid; the existing last-known-good copy is kept.'
        return $false
    }
    try {
        Copy-Item -LiteralPath $script:EnvPath -Destination $script:LastGoodPath -Force
        Protect-ConfigFile -Path $script:LastGoodPath
        Write-Ok 'Saved a protected last-known-good configuration copy.'
        return $true
    }
    catch {
        Write-Warn ('Could not save a last-known-good copy: ' + $_.Exception.Message)
        return $false
    }
}

function Read-EnvLines {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        return @()
    }
    return @(Get-Content -LiteralPath $Path -Encoding UTF8)
}

function Write-EnvLines {
    param([string]$Path, [string[]]$Lines)
    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, $Lines, $encoding)
    Protect-ConfigFile -Path $Path
}

function Set-EnvValue {
    param([string]$Key, [string]$Value)
    $lines = Read-EnvLines -Path $script:EnvPath
    $updated = New-Object System.Collections.Generic.List[string]
    $replaced = $false
    foreach ($line in $lines) {
        if ($line -match ('^\s*' + [regex]::Escape($Key) + '\s*=')) {
            $updated.Add($Key + '=' + $Value)
            $replaced = $true
        }
        else {
            $updated.Add($line)
        }
    }
    if (-not $replaced) {
        $updated.Add($Key + '=' + $Value)
    }
    Write-EnvLines -Path $script:EnvPath -Lines $updated.ToArray()
}

function Get-NonModelDifferences {
    param([hashtable]$Before, [hashtable]$After)
    $keys = New-Object System.Collections.Generic.List[string]
    foreach ($key in $Before.Keys) { if (-not $keys.Contains($key)) { $keys.Add($key) } }
    foreach ($key in $After.Keys) { if (-not $keys.Contains($key)) { $keys.Add($key) } }
    $changed = New-Object System.Collections.Generic.List[string]
    foreach ($key in $keys) {
        if ($key.StartsWith('MODEL_')) { continue }
        $beforeValue = ''
        if ($Before.ContainsKey($key)) { $beforeValue = [string]$Before[$key] }
        $afterValue = ''
        if ($After.ContainsKey($key)) { $afterValue = [string]$After[$key] }
        if ($beforeValue -ne $afterValue) { $changed.Add($key) }
    }
    return $changed.ToArray()
}

function Get-DockerHealth {
    $result = [pscustomobject]@{
        Installed      = $false
        ClientVersion  = ''
        Engine         = $false
        ServerVersion  = ''
        Compose        = $false
        ComposeVersion = ''
    }
    $docker = Get-Command docker -ErrorAction SilentlyContinue
    if (-not $docker) {
        return $result
    }
    $result.Installed = $true
    $client = & docker --version 2>$null
    if ($client) {
        $result.ClientVersion = (($client | Out-String).Trim())
    }
    $server = & docker info --format '{{.ServerVersion}}' 2>$null
    if ($LASTEXITCODE -eq 0 -and $server) {
        $result.Engine = $true
        $result.ServerVersion = (($server | Out-String).Trim())
    }
    $compose = & docker compose version --short 2>$null
    if ($LASTEXITCODE -eq 0 -and $compose) {
        $result.Compose = $true
        $result.ComposeVersion = (($compose | Out-String).Trim())
    }
    return $result
}

function Test-SamePath {
    param([string]$Left, [string]$Right)
    try {
        $a = [System.IO.Path]::GetFullPath($Left).TrimEnd('\', '/')
        $b = [System.IO.Path]::GetFullPath($Right).TrimEnd('\', '/')
        return [string]::Equals($a, $b, [System.StringComparison]::OrdinalIgnoreCase)
    }
    catch {
        return $false
    }
}

function Get-ForeignProjectPaths {
    # The Compose project name is fixed, so every checkout on one Docker engine
    # shares container names, port 8080, and the database volume. Never adopt,
    # stop, or reconfigure containers whose working directory is another copy.
    $ids = & docker ps -a --filter 'label=com.docker.compose.project=derive-world' --format '{{.ID}}' 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $ids) {
        return @()
    }
    $foreign = New-Object System.Collections.Generic.List[string]
    foreach ($id in $ids) {
        $raw = & docker inspect $id 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $raw) {
            $foreign.Add('unknown container ' + $id)
            continue
        }
        try {
            $inspect = (($raw | Out-String).Trim() | ConvertFrom-Json)
            $details = @($inspect)[0]
            $labels = $details.Config.Labels
            $workingDir = ''
            $configFile = ''
            if ($null -ne $labels) {
                if ($labels.PSObject.Properties.Name -contains 'com.docker.compose.project.working_dir') {
                    $workingDir = [string]$labels.'com.docker.compose.project.working_dir'
                }
                if ($labels.PSObject.Properties.Name -contains 'com.docker.compose.project.config_files') {
                    $configFile = [string]$labels.'com.docker.compose.project.config_files'
                }
            }
            if ($workingDir -ne '' -and -not (Test-SamePath $workingDir $script:Root)) {
                $foreign.Add($workingDir)
                continue
            }
            if ($workingDir -eq '' -and $configFile -ne '') {
                $configPath = $configFile.Split(',')[0].Trim()
                $configDir = Split-Path -Parent $configPath
                if ($configDir -ne '' -and -not (Test-SamePath $configDir $script:Root)) {
                    $foreign.Add($configDir)
                }
            }
        }
        catch {
            $foreign.Add('unreadable container ' + $id)
        }
    }
    return @($foreign | Select-Object -Unique)
}

function Test-InstanceOwnership {
    param([switch]$Quiet)
    $foreign = Get-ForeignProjectPaths
    if (@($foreign).Count -eq 0) {
        return $true
    }
    if (-not $Quiet) {
        Write-Fail 'Another Derive World checkout is using the same Compose project and database volume.'
        foreach ($path in $foreign) {
            Write-Info ('  foreign working directory: ' + $path)
        }
        Write-Info 'Stop that copy from its own directory first. This launcher will not touch it.'
        Write-Info 'The Compose project name and database volume are shared per Docker engine.'
    }
    return $false
}

function Get-ProductUrl {
    $map = Read-EnvFile -Path $script:EnvPath
    $value = Get-EnvValue -Map $map -Key 'PUBLIC_BASE_URL'
    if ($value -ne '') {
        try {
            $uri = [Uri]$value
            if ($uri.IsAbsoluteUri -and ($uri.Scheme -eq 'http' -or $uri.Scheme -eq 'https')) {
                return ($uri.Scheme + '://' + $uri.Authority)
            }
        }
        catch {
            # Fall through to the local URL.
        }
    }
    return $script:AppUrl
}

function Test-RequiredConfig {
    param([hashtable]$Map)
    $errors = New-Object System.Collections.Generic.List[string]
    $appEnv = Get-EnvValue -Map $Map -Key 'APP_ENV'
    if ($appEnv -eq '') {
        $errors.Add('APP_ENV is required.')
    }
    foreach ($key in @('CURSOR_HMAC_SECRET', 'IP_HASH_PEPPER', 'CAPABILITY_HMAC_SECRET')) {
        $value = Get-EnvValue -Map $Map -Key $key
        if ($value -eq '') {
            $errors.Add($key + ' is required.')
        }
        elseif ($value.Length -lt 32) {
            $errors.Add($key + ' must be at least 32 bytes.')
        }
    }
    $publicURL = Get-EnvValue -Map $Map -Key 'PUBLIC_BASE_URL'
    if ($publicURL -eq '') {
        $errors.Add('PUBLIC_BASE_URL is required.')
    }
    else {
        try {
            $uri = [Uri]$publicURL
            if (-not $uri.IsAbsoluteUri -or ($uri.Scheme -ne 'http' -and $uri.Scheme -ne 'https') -or $uri.Host -eq '') {
                $errors.Add('PUBLIC_BASE_URL must be an absolute http(s) origin.')
            }
        }
        catch {
            $errors.Add('PUBLIC_BASE_URL must be an absolute http(s) origin.')
        }
    }
    $modelCheck = Test-ModelConfig -Map $Map
    foreach ($item in $modelCheck.Errors) {
        $errors.Add($item)
    }
    return $errors.ToArray()
}

function Test-PortListening {
    param([int]$Port)
    try {
        $connections = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop
        return (@($connections).Count -gt 0)
    }
    catch {
        $matches = & netstat.exe -ano 2>$null | Select-String -Pattern (':' + $Port + '\s+.*LISTENING')
        return ($null -ne $matches -and @($matches).Count -gt 0)
    }
}

function Test-DataVolume {
    $volumes = & docker volume ls --format '{{.Name}}' 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $volumes) {
        return $false
    }
    return (@($volumes | Where-Object { $_ -like '*dw_pgdata*' }).Count -gt 0)
}

function Get-HttpDocument {
    param([string]$Url, [int]$TimeoutSeconds = 5)
    try {
        $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec $TimeoutSeconds
        return [pscustomobject]@{ Ok = $true; StatusCode = [int]$response.StatusCode; Body = [string]$response.Content }
    }
    catch {
        return [pscustomobject]@{ Ok = $false; StatusCode = 0; Body = $_.Exception.Message }
    }
}

function Get-ProductState {
    $health = Get-HttpDocument -Url ($script:AppUrl + '/healthz') -TimeoutSeconds 3
    $ready = Get-HttpDocument -Url ($script:AppUrl + '/readyz') -TimeoutSeconds 3
    $meta = Get-HttpDocument -Url ($script:AppUrl + '/api/v1/meta') -TimeoutSeconds 3
    $modelMode = ''
    $version = ''
    $commit = ''
    $buildTime = ''
    if ($meta.Ok) {
        try {
            $parsed = $meta.Body | ConvertFrom-Json
            if ($parsed.PSObject.Properties.Name -contains 'model_mode') {
                $modelMode = [string]$parsed.model_mode
            }
            if ($parsed.PSObject.Properties.Name -contains 'version') {
                $version = [string]$parsed.version
            }
            if ($parsed.PSObject.Properties.Name -contains 'commit') {
                $commit = [string]$parsed.commit
            }
            if ($parsed.PSObject.Properties.Name -contains 'build_time') {
                $buildTime = [string]$parsed.build_time
            }
        }
        catch {
            $modelMode = ''
        }
    }
    return [pscustomobject]@{
        Health    = $health.Ok
        Ready     = $ready.Ok
        Meta      = $meta.Ok
        ModelMode = $modelMode
        Version   = $version
        Commit    = $commit
        BuildTime = $buildTime
    }
}

function Wait-ForReady {
    param([int]$TimeoutSeconds)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $health = Get-HttpDocument -Url ($script:AppUrl + '/readyz') -TimeoutSeconds 3
        if ($health.Ok) {
            return $true
        }
        Start-Sleep -Seconds 3
    }
    return $false
}

function Invoke-Compose {
    param([string[]]$ComposeArguments)
    $joined = ($ComposeArguments -join ' ').ToLowerInvariant()
    foreach ($forbidden in @('-v', '--volumes', 'volume rm', 'volume prune', 'system prune', 'compose rm')) {
        if ($joined -match ('(^|\s)' + [regex]::Escape($forbidden) + '(\s|$)')) {
            return [pscustomobject]@{ ExitCode = 90; Output = 'Refused destructive Compose argument: ' + $forbidden }
        }
    }
    $baseArguments = @('compose', '--project-directory', $script:Root, '-f', $script:ComposePath)
    if (Test-Path -LiteralPath $script:EnvPath) {
        $baseArguments += @('--env-file', $script:EnvPath)
    }
    Push-Location $script:Root
    try {
        $output = & docker @baseArguments @ComposeArguments 2>&1
        $code = $LASTEXITCODE
        return [pscustomobject]@{ ExitCode = $code; Output = (($output | Out-String).Trim()) }
    }
    finally {
        Pop-Location
    }
}

function Test-ModelConfig {
    param([hashtable]$Map)
    $errors = New-Object System.Collections.Generic.List[string]
    $warnings = New-Object System.Collections.Generic.List[string]
    $mode = (Get-EnvValue -Map $Map -Key 'MODEL_MODE').ToLowerInvariant()
    if ($mode -eq '') {
        $mode = 'fixture'
    }
    if ($mode -ne 'fixture' -and $mode -ne 'real') {
        $errors.Add('MODEL_MODE must be "fixture" or "real".')
        return [pscustomobject]@{ Mode = $mode; Ok = $false; Errors = $errors; Warnings = $warnings }
    }
    if ($mode -eq 'fixture') {
        return [pscustomobject]@{ Mode = $mode; Ok = $true; Errors = $errors; Warnings = $warnings }
    }
    $baseUrl = Get-EnvValue -Map $Map -Key 'MODEL_BASE_URL'
    $modelName = Get-EnvValue -Map $Map -Key 'MODEL_NAME'
    $apiKey = Get-EnvValue -Map $Map -Key 'MODEL_API_KEY'
    if ($baseUrl -eq '') {
        $errors.Add('MODEL_BASE_URL is required in real mode.')
    }
    else {
        try {
            $uri = [Uri]$baseUrl
            if (-not $uri.IsAbsoluteUri) {
                $errors.Add('MODEL_BASE_URL must be an absolute URL.')
            }
            elseif ($uri.Scheme -ne 'http' -and $uri.Scheme -ne 'https') {
                $errors.Add('MODEL_BASE_URL must use http or https.')
            }
            elseif ($uri.Host -eq '') {
                $errors.Add('MODEL_BASE_URL must include a host.')
            }
            elseif ($uri.Query -ne '' -or $uri.Fragment -ne '') {
                $errors.Add('MODEL_BASE_URL must not include a query string or fragment.')
            }
            else {
                if ($uri.Scheme -eq 'http' -and $uri.Host -ne '127.0.0.1' -and $uri.Host -ne 'localhost' -and $uri.Host -ne '::1') {
                    $warnings.Add('MODEL_BASE_URL uses http on a non-loopback host; the API key would travel without TLS.')
                }
            }
        }
        catch {
            $errors.Add('MODEL_BASE_URL is not a valid URL.')
        }
    }
    if ($modelName.Trim() -eq '') {
        $errors.Add('MODEL_NAME is required in real mode.')
    }
    if ($apiKey.Trim() -eq '') {
        $errors.Add('MODEL_API_KEY is required in real mode.')
    }
    elseif ($apiKey -match '\s') {
        $errors.Add('MODEL_API_KEY must not contain whitespace.')
    }
    return [pscustomobject]@{
        Mode     = $mode
        Ok       = ($errors.Count -eq 0)
        Errors   = $errors
        Warnings = $warnings
    }
}

function Test-EndpointReachability {
    param([string]$BaseUrl)
    try {
        $uri = [Uri]$BaseUrl
    }
    catch {
        return [pscustomobject]@{ Ok = $false; Detail = 'The model base URL is not a valid URL.' }
    }
    if (-not $uri.IsAbsoluteUri -or $uri.Host -eq '') {
        return [pscustomobject]@{ Ok = $false; Detail = 'The model base URL is not an absolute URL.' }
    }
    $port = $uri.Port
    if ($port -le 0) {
        if ($uri.Scheme -eq 'https') { $port = 443 } else { $port = 80 }
    }
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $async = $client.BeginConnect($uri.Host, $port, $null, $null)
        if (-not $async.AsyncWaitHandle.WaitOne(5000)) {
            return [pscustomobject]@{ Ok = $false; Detail = ('TCP connection to ' + $uri.Host + ':' + $port + ' timed out.') }
        }
        $client.EndConnect($async)
        if ($uri.Scheme -eq 'https') {
            $ssl = New-Object System.Net.Security.SslStream($client.GetStream(), $false)
            try {
                $ssl.AuthenticateAsClient($uri.Host)
            }
            catch {
                return [pscustomobject]@{ Ok = $false; Detail = ('TLS verification failed: ' + $_.Exception.Message) }
            }
            finally {
                $ssl.Dispose()
            }
        }
        return [pscustomobject]@{ Ok = $true; Detail = ('Endpoint ' + $uri.Host + ':' + $port + ' reachable. Model name is not verified (no inference call).') }
    }
    catch {
        return [pscustomobject]@{ Ok = $false; Detail = ('Endpoint unreachable: ' + $_.Exception.Message) }
    }
    finally {
        $client.Close()
    }
}

function Mask-Diagnostics {
    param([string]$Text, [hashtable]$Map)
    $masked = $Text
    foreach ($key in @('MODEL_API_KEY', 'CURSOR_HMAC_SECRET', 'IP_HASH_PEPPER', 'CAPABILITY_HMAC_SECRET', 'METRICS_TOKEN')) {
        $value = Get-EnvValue -Map $Map -Key $key
        if ($value.Length -ge 4) {
            $masked = $masked.Replace($value, '***')
        }
    }
    $masked = [regex]::Replace($masked, '(?im)^(\s*[A-Z0-9_]*(?:API_KEY|SECRET|PASSWORD|TOKEN|DATABASE_URL)[A-Z0-9_]*\s*=\s*).*$', '$1***')
    $masked = [regex]::Replace($masked, '(?i)(authorization\s*[:=]\s*).*$', '$1***')
    $masked = [regex]::Replace($masked, '(?i)(cookie\s*[:=]\s*).*$', '$1***')
    $masked = [regex]::Replace($masked, '(?i)(bearer\s+)[A-Za-z0-9._\-]+', '$1***')
    $masked = [regex]::Replace($masked, 'dw_pat_[A-Za-z0-9_\-]+', 'dw_pat_***')
    $masked = [regex]::Replace($masked, '[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}', '***@***')
    return $masked
}

function Show-PrerequisiteReport {
    param([switch]$AllowMissingDocker)
    $docker = Get-DockerHealth
    $failed = $false
    $warned = $false

    if ($docker.Installed) {
        Write-Ok ('Docker CLI found: ' + $docker.ClientVersion)
    }
    else {
        Write-Fail 'Docker is not installed or not on PATH.'
        Write-Info 'Install Docker Desktop: https://www.docker.com/products/docker-desktop/'
        Write-Info 'After installing, start Docker Desktop and run this check again.'
        $failed = $true
    }
    if ($docker.Engine) {
        Write-Ok ('Docker Engine is running: ' + $docker.ServerVersion)
    }
    else {
        Write-Fail 'Docker Engine is not available.'
        Write-Info 'Start Docker Desktop and wait for "Engine running", then run this check again.'
        $failed = $true
    }
    if ($docker.Compose) {
        Write-Ok ('Docker Compose found: ' + $docker.ComposeVersion)
    }
    else {
        Write-Fail 'Docker Compose v2 is not available.'
        Write-Info 'Compose ships with Docker Desktop. Update Docker Desktop if "docker compose version" fails.'
        $failed = $true
    }
    if (Test-Path -LiteralPath $script:ComposePath) {
        Write-Ok 'compose.yaml found for this instance.'
    }
    else {
        Write-Fail 'compose.yaml is missing. Run the launcher from the repository root.'
        $failed = $true
    }
    if (Test-Path -LiteralPath $script:EnvPath) {
        Write-Ok '.env found. It is intentionally git-ignored.'
        $configErrors = @(Test-RequiredConfig -Map (Read-EnvFile -Path $script:EnvPath))
        if ($configErrors.Count -eq 0) {
            Write-Ok 'Required configuration keys and model settings are present.'
        }
        else {
            foreach ($item in $configErrors) {
                Write-Fail ('Configuration: ' + $item)
            }
            Write-Info 'Fix .env with "AI settings / Connect AI" or a text editor. Values are never printed.'
            $failed = $true
        }
    }
    else {
        Write-Warn '.env does not exist yet; Start will create it from .env.example in Fixture mode.'
        $warned = $true
    }
    try {
        $probe = Join-Path $script:Root ('.dw-write-probe-' + [Guid]::NewGuid().ToString('N') + '.tmp')
        Set-Content -LiteralPath $probe -Value 'probe' -Encoding ASCII
        Remove-Item -LiteralPath $probe -Force
        Write-Ok 'The instance directory is writable.'
    }
    catch {
        Write-Fail ('The instance directory is not writable: ' + $_.Exception.Message)
        $failed = $true
    }
    if ($docker.Engine) {
        if (Test-DataVolume) {
            Write-Ok 'Database volume exists; Start/Stop/Restart will preserve it.'
        }
        else {
            Write-Warn 'No database volume yet; the first start will create one and run migrations.'
            $warned = $true
        }
        if (Test-InstanceOwnership -Quiet) {
            Write-Ok 'No other checkout owns the shared Compose project.'
        }
        else {
            Write-Fail 'Another checkout owns the shared Compose project; this launcher will not mutate it.'
            foreach ($path in (Get-ForeignProjectPaths)) {
                Write-Info ('  foreign working directory: ' + $path)
            }
            Write-Info 'Stop that copy from its own directory first.'
            $failed = $true
        }
    }
    if (Test-PortListening -Port $script:AppPort) {
        $state = Get-ProductState
        if ($state.Health) {
            Write-Ok ('Port ' + $script:AppPort + ' is already serving Derive World; the instance is running.')
        }
        else {
            Write-Fail ('Port ' + $script:AppPort + ' is in use by another program. Free it before starting.')
            $failed = $true
        }
    }
    else {
        Write-Ok ('Port ' + $script:AppPort + ' is free.')
    }
    if ($env:GOPROXY) {
        Write-Info ('GOPROXY is set in this shell: ' + $env:GOPROXY)
    }
    if ($failed -and $AllowMissingDocker -and (-not $docker.Installed -or -not $docker.Engine)) {
        Write-Warn 'Docker is not available in this environment; continuing because -AllowMissingDocker was requested.'
        return $true
    }
    if ($failed) {
        return $false
    }
    if ($warned) {
        Write-Info 'Some optional checks reported warnings; Start will handle them.'
    }
    return $true
}

function Show-Status {
    $docker = Get-DockerHealth
    if (-not $docker.Installed) {
        Write-Fail 'Docker is not installed.'
        return $false
    }
    if (-not $docker.Engine) {
        Write-Fail 'Docker Engine is not running.'
        return $false
    }
    Push-Location $script:Root
    try {
        Write-Step 'Compose services'
        & docker compose ps
    }
    finally {
        Pop-Location
    }
    $state = Get-ProductState
    if ($state.Health) {
        Write-Ok 'Health endpoint is responding.'
    }
    else {
        Write-Fail 'Health endpoint is not responding.'
    }
    if ($state.Ready) {
        Write-Ok 'Readiness endpoint is responding (database and dependencies ready).'
    }
    else {
        Write-Fail 'Readiness endpoint is not responding.'
    }
    if ($state.Meta) {
        Write-Ok ('Product API is responding. Version: ' + $state.Version + '; model mode: ' + $state.ModelMode)
        Write-Info ('Commit: ' + $(if ($state.Commit -eq '' -or $state.Commit -eq 'unknown') { 'unknown (not a verified release build)' } else { $state.Commit }))
        if ($state.ModelMode -eq 'fixture') {
            Write-Info 'Fixture mode: AI replies are deterministic demo output; created objects are real.'
        }
    }
    else {
        Write-Warn 'Product API /api/v1/meta is not responding yet.'
    }
    if (-not (Test-InstanceOwnership -Quiet)) {
        Write-Fail 'The running Compose project belongs to another checkout; do not use Stop/Restart here.'
        foreach ($path in (Get-ForeignProjectPaths)) {
            Write-Info ('  foreign working directory: ' + $path)
        }
    }
    return ($state.Health -and $state.Ready)
}

function Show-AIConfigStatus {
    $map = Read-EnvFile -Path $script:EnvPath
    $mode = (Get-EnvValue -Map $map -Key 'MODEL_MODE')
    if ($mode -eq '') { $mode = 'fixture' }
    $baseUrl = Get-EnvValue -Map $map -Key 'MODEL_BASE_URL'
    $modelName = Get-EnvValue -Map $map -Key 'MODEL_NAME'
    $apiKey = Get-EnvValue -Map $map -Key 'MODEL_API_KEY'
    Write-Info ('MODEL_MODE      = ' + $mode)
    Write-Info ('MODEL_BASE_URL  = ' + $(if ($baseUrl -eq '') { '(empty)' } else { $baseUrl }))
    Write-Info ('MODEL_NAME      = ' + $(if ($modelName -eq '') { '(empty)' } else { $modelName }))
    Write-Info ('MODEL_API_KEY   = ' + (Get-MaskedValue -Value $apiKey))
    $check = Test-ModelConfig -Map $map
    if (-not $check.Ok) {
        foreach ($item in $check.Errors) {
            Write-Fail $item
        }
    }
    foreach ($item in $check.Warnings) {
        Write-Warn $item
    }
    if ($check.Ok) {
        Write-Ok 'The model configuration has the required fields.'
        if ($check.Mode -eq 'real' -and $baseUrl -ne '') {
            $reachable = Test-EndpointReachability -BaseUrl $baseUrl
            if ($reachable.Ok) {
                Write-Ok $reachable.Detail
            }
            else {
                Write-Warn $reachable.Detail
            }
        }
    }
}

function Open-AIConfig {
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    Protect-ConfigFile -Path $script:EnvPath
    [void](Save-LastGoodConfig)
    Write-Title 'AI settings / Connect AI'
    Write-Info 'Edit ONLY these lines in the file that opens:'
    Write-Info ''
    Write-Info '  MODEL_MODE=real'
    Write-Info '  MODEL_BASE_URL=https://your-provider.example'
    Write-Info '  MODEL_NAME=your-model-name'
    Write-Info '  MODEL_API_KEY=your-api-key'
    Write-Info ''
    Write-Info 'Notes:'
    Write-Info '  - MODEL_BASE_URL is the provider root WITHOUT /v1 and without a trailing slash.'
    Write-Info '  - The API key is stored only in this instance .env file, never in the browser.'
    Write-Info '  - Save the file and close Notepad, then choose "Apply AI settings".'
    Write-Info '  - Use MODEL_MODE=fixture to return to the deterministic demo at any time.'
    Write-Info '  - .env is not an encrypted vault; anyone with local or Docker admin rights can read it.'
    Write-Info ''
    if ($script:IsWindowsHost) {
        try {
            Start-Process -FilePath 'notepad.exe' -ArgumentList ('"' + $script:EnvPath + '"') -Wait
            Protect-ConfigFile -Path $script:EnvPath
            Write-Ok 'The configuration file was closed and its permissions were re-checked.'
            return $true
        }
        catch {
            Write-Fail ('Could not open Notepad: ' + $_.Exception.Message)
            Write-Info ('Open this file manually: ' + $script:EnvPath)
            return $false
        }
    }
    Write-Info ('Open this file with your editor: ' + $script:EnvPath)
    return $true
}

function Confirm-Action {
    param([string]$Prompt, [bool]$DefaultYes = $true)
    if ($Force) {
        return $true
    }
    $suffix = '[y/N]'
    if ($DefaultYes) { $suffix = '[Y/n]' }
    $answer = Read-Host ($Prompt + ' ' + $suffix)
    if ($answer -eq '') {
        return $DefaultYes
    }
    return ($answer -match '^(y|yes)$')
}

function Restore-EnvBackup {
    param([string]$BackupPath)
    if ($BackupPath -eq '' -or -not (Test-Path -LiteralPath $BackupPath)) {
        Write-Fail 'No configuration backup is available to restore.'
        return $false
    }
    Copy-Item -LiteralPath $BackupPath -Destination $script:EnvPath -Force
    Protect-ConfigFile -Path $script:EnvPath
    Write-Ok ('Restored the previous configuration from ' + (Split-Path -Leaf $BackupPath) + '.')
    return $true
}

function Apply-AIConfig {
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    Protect-ConfigFile -Path $script:EnvPath
    $map = Read-EnvFile -Path $script:EnvPath
    $check = Test-ModelConfig -Map $map
    $requiredErrors = @(Test-RequiredConfig -Map $map)
    Write-Title 'Apply AI settings'
    if (-not $check.Ok -or $requiredErrors.Count -gt 0) {
        foreach ($item in $requiredErrors) {
            Write-Fail $item
        }
        if (-not $check.Ok) {
            foreach ($item in $check.Errors) {
                Write-Fail $item
            }
        }
        Write-Info 'Nothing was restarted. Fix the listed fields in .env, then apply again.'
        return $false
    }
    if (Test-Path -LiteralPath $script:LastGoodPath) {
        $lastGood = Read-EnvFile -Path $script:LastGoodPath
        $diff = @(Get-NonModelDifferences -Before $lastGood -After $map)
        if ($diff.Count -gt 0) {
            Write-Warn ('Non-model settings changed: ' + ($diff -join ', '))
            Write-Info 'Only MODEL_* lines need to change for AI setup. Changing other keys can break sessions, capabilities, or origin checks.'
            if (-not (Confirm-Action -Prompt 'Apply changes to non-model settings as well?' -DefaultYes $false)) {
                Write-Info 'Apply cancelled. No service was restarted.'
                return $false
            }
        }
    }
    else {
        Write-Warn 'No last-known-good configuration copy exists. Recovery on failure will be limited.'
        if (-not (Confirm-Action -Prompt 'Continue without a last-known-good copy?' -DefaultYes $false)) {
            Write-Info 'Apply cancelled. Open the AI configuration file first to save a recovery copy.'
            return $false
        }
    }
    foreach ($item in $check.Warnings) {
        Write-Warn $item
    }
    if ($check.Mode -eq 'real') {
        $reachable = Test-EndpointReachability -BaseUrl (Get-EnvValue -Map $map -Key 'MODEL_BASE_URL')
        if ($reachable.Ok) {
            Write-Ok $reachable.Detail
        }
        else {
            Write-Warn $reachable.Detail
            if (-not (Confirm-Action -Prompt 'The endpoint check did not pass. Apply anyway?' -DefaultYes $false)) {
                Write-Info 'Apply cancelled. No service was restarted and no file was changed.'
                return $false
            }
        }
    }
    $docker = Get-DockerHealth
    if (-not $docker.Installed -or -not $docker.Engine -or -not $docker.Compose) {
        Write-Fail 'Docker, the Docker Engine, and Compose must be available before applying settings.'
        return $false
    }
    if (-not (Test-InstanceOwnership)) {
        return $false
    }
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmssfff'
    $backup = Join-Path $script:Root ('.env.backup-' + $stamp)
    try {
        Copy-Item -LiteralPath $script:EnvPath -Destination $backup
        Protect-ConfigFile -Path $backup
        Write-Ok ('Saved a configuration snapshot: ' + (Split-Path -Leaf $backup))
    }
    catch {
        Write-Fail ('Could not create a configuration snapshot; no service was restarted: ' + $_.Exception.Message)
        return $false
    }
    $recovery = $backup
    if (Test-Path -LiteralPath $script:LastGoodPath) {
        $recovery = $script:LastGoodPath
    }
    Write-Step 'Asking the server to validate exactly this configuration...'
    $configCheck = Invoke-Compose -ComposeArguments @('run', '--rm', '--no-deps', '-T', 'app', 'check-config')
    if ($configCheck.ExitCode -eq 2) {
        Write-Fail 'The server rejected this configuration. Nothing was restarted.'
        Write-Info (Mask-Diagnostics -Text $configCheck.Output -Map $map)
        return $false
    }
    if ($configCheck.ExitCode -eq 0) {
        Write-Ok 'The server accepted the configuration.'
    }
    else {
        Write-Warn 'The server-side configuration check could not run (the image may not be built yet).'
    }
    Write-Step 'Recreating the application container with the new settings (data is preserved)...'
    $recreate = Invoke-Compose -ComposeArguments @('up', '-d', '--force-recreate', 'app')
    if ($recreate.ExitCode -ne 0) {
        Write-Fail 'The application container did not start.'
        Write-Info (Mask-Diagnostics -Text $recreate.Output -Map $map)
        if (Confirm-Action -Prompt 'Restore the last-known-good configuration now?' -DefaultYes $true) {
            if (Restore-EnvBackup -BackupPath $recovery) {
                [void](Invoke-Compose -ComposeArguments @('up', '-d', '--force-recreate', 'app'))
                [void](Wait-ForReady -TimeoutSeconds 60)
            }
        }
        return $false
    }
    Write-Step 'Waiting for readiness...'
    $ready = Wait-ForReady -TimeoutSeconds $HealthTimeoutSeconds
    $state = Get-ProductState
    $expectedMode = $check.Mode
    $modeOk = ($state.Meta -and $state.ModelMode -eq $expectedMode)
    if (-not $ready -or -not $modeOk) {
        Write-Fail 'The new settings did not become healthy and active.'
        if ($state.Meta) {
            Write-Info ('Reported model mode: ' + $state.ModelMode + '; expected: ' + $expectedMode)
        }
        Write-Step 'Restoring the last-known-good configuration automatically...'
        if (Restore-EnvBackup -BackupPath $recovery) {
            $restart = Invoke-Compose -ComposeArguments @('up', '-d', '--force-recreate', 'app')
            if ($restart.ExitCode -eq 0 -and (Wait-ForReady -TimeoutSeconds 60)) {
                Write-Ok 'The previous configuration is healthy again. No data was deleted.'
            }
            else {
                Write-Fail 'The previous configuration also failed to become ready.'
                Write-Info 'Run "Diagnostics" and check Docker Desktop. Do not delete the database volume.'
            }
        }
        return $false
    }
    if ($expectedMode -eq 'real') {
        Write-Step 'Running the non-inference model check inside the instance...'
        $modelCheck = Invoke-Compose -ComposeArguments @('run', '--rm', '--no-deps', '-T', 'app', 'check-model')
        $modelOutput = (($modelCheck.Output -split "`n") | Where-Object { $_ -match 'model check:' } | Select-Object -First 1)
        if ($modelOutput) {
            Write-Info ($modelOutput.Trim())
        }
        if ($modelOutput -match 'unauthorized|tls_failure|invalid_url') {
            Write-Fail 'The provider rejected the configuration. Restoring the last-known-good configuration...'
            if (Restore-EnvBackup -BackupPath $recovery) {
                $restart = Invoke-Compose -ComposeArguments @('up', '-d', '--force-recreate', 'app')
                if ($restart.ExitCode -eq 0 -and (Wait-ForReady -TimeoutSeconds 60)) {
                    Write-Ok 'The previous configuration is healthy again. No data was deleted.'
                }
            }
            return $false
        }
        if ($modelOutput -match 'unreachable|rate_limited|unavailable') {
            Write-Warn 'The provider is currently unreachable or busy. The configuration is saved; retry later from Settings.'
        }
        if ($modelOutput -match 'unverified_model') {
            Write-Info 'The models endpoint answered. This does not prove the configured model is callable.'
        }
    }
    [void](Save-LastGoodConfig)
    Write-Ok ('Settings applied. Model mode is now "' + $state.ModelMode + '".')
    if ($state.ModelMode -eq 'real') {
        Write-Info 'No inference call was made during setup, so no provider completion charges were created.'
        Write-Info 'A real AI call still requires an authorized action from Settings or the AI panel.'
    }
    Write-Info 'Old .env.backup-* snapshots contain plaintext secrets; delete them once you are satisfied.'
    return $true
}

function Use-FixtureMode {
    Write-Title 'Switch AI to Fixture mode'
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    [void](Save-LastGoodConfig)
    Set-EnvValue -Key 'MODEL_MODE' -Value 'fixture'
    Write-Ok 'MODEL_MODE is now fixture. The existing key fields were left untouched.'
    return (Apply-AIConfig)
}

function Start-Instance {
    Write-Title 'Start Derive World'
    if (-not (Test-InstanceOwnership)) {
        return $false
    }
    if (Test-PortListening -Port $script:AppPort) {
        $state = Get-ProductState
        if ($state.Health) {
            Write-Ok 'Derive World is already running.'
            if (-not $NoBrowser) {
                Start-Process (Get-ProductUrl)
            }
            return $true
        }
        Write-Fail ('Port ' + $script:AppPort + ' is used by another program. Free it first.')
        return $false
    }
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    $map = Read-EnvFile -Path $script:EnvPath
    $requiredErrors = @(Test-RequiredConfig -Map $map)
    if ($requiredErrors.Count -gt 0) {
        foreach ($item in $requiredErrors) {
            Write-Fail ('Configuration: ' + $item)
        }
        Write-Info 'Nothing was started. Fix .env with "AI settings / Connect AI". Values are never printed.'
        return $false
    }
    $check = Test-ModelConfig -Map $map
    if (-not $check.Ok) {
        Write-Warn 'The saved model configuration is incomplete. The stack may fail to start in Real mode.'
        foreach ($item in $check.Errors) {
            Write-Warn $item
        }
        Write-Info 'Use "AI settings / Connect AI" to fix it. Fixture mode needs no key.'
    }
    if (-not (Show-PrerequisiteReport)) {
        return $false
    }
    Write-Step 'Starting the stack. The first build can take several minutes...'
    $up = Invoke-Compose -ComposeArguments @('up', '-d', '--build')
    if ($up.ExitCode -ne 0) {
        Write-Fail 'Docker Compose did not start the stack.'
        Write-Info (Mask-Diagnostics -Text $up.Output -Map $map)
        return $false
    }
    Write-Step 'Waiting for readiness...'
    if (-not (Wait-ForReady -TimeoutSeconds $HealthTimeoutSeconds)) {
        Write-Fail 'The stack started but /readyz did not become ready in time.'
        [void](Show-Status)
        Write-Info 'Run "Diagnostics" for redacted logs. Do not delete the database volume.'
        return $false
    }
    $state = Get-ProductState
    $productUrl = Get-ProductUrl
    Write-Ok ('Derive World is ready at ' + $productUrl)
    if ($state.Version -ne '') {
        Write-Info ('Version: ' + $state.Version + '; commit: ' + $(if ($state.Commit -eq '' -or $state.Commit -eq 'unknown') { 'unknown' } else { $state.Commit }) + '; built: ' + $(if ($state.BuildTime -eq '' -or $state.BuildTime -eq 'unknown') { 'unknown' } else { $state.BuildTime }))
    }
    if ($state.ModelMode -eq 'fixture') {
        Write-Info 'Fixture mode: AI replies are deterministic demo output, clearly marked FIXTURE.'
        Write-Info 'Objects you create are real and saved in this instance.'
    }
    else {
        Write-Info ('Model mode: ' + $state.ModelMode)
    }
    if (-not $NoBrowser) {
        Start-Process $productUrl
    }
    return $true
}

function Stop-Instance {
    Write-Title 'Stop Derive World'
    $docker = Get-DockerHealth
    if (-not $docker.Installed -or -not $docker.Engine -or -not $docker.Compose) {
        Write-Fail 'Docker, the Engine, and Compose must be available.'
        return $false
    }
    if (-not (Test-InstanceOwnership)) {
        return $false
    }
    Write-Step 'Stopping services. Containers and the database volume are preserved.'
    $stop = Invoke-Compose -ComposeArguments @('stop')
    if ($stop.ExitCode -ne 0) {
        Write-Fail 'Docker Compose could not stop the stack.'
        Write-Info $stop.Output
        return $false
    }
    Write-Ok 'The stack is stopped. Your world data remains in the Docker volume.'
    return $true
}

function Restart-Instance {
    Write-Title 'Restart Derive World'
    $docker = Get-DockerHealth
    if (-not $docker.Installed -or -not $docker.Engine -or -not $docker.Compose) {
        Write-Fail 'Docker, the Engine, and Compose must be available.'
        return $false
    }
    if (-not (Test-InstanceOwnership)) {
        return $false
    }
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    Write-Step 'Recreating the application container. Database data is preserved.'
    $up = Invoke-Compose -ComposeArguments @('up', '-d', '--force-recreate', 'app')
    if ($up.ExitCode -ne 0) {
        Write-Fail 'The application container did not start.'
        return $false
    }
    if (-not (Wait-ForReady -TimeoutSeconds $HealthTimeoutSeconds)) {
        Write-Fail 'The product did not become ready.'
        return $false
    }
    Write-Ok 'Restarted and ready.'
    return $true
}

function Show-Diagnostics {
    Write-Title 'Redacted diagnostics'
    if (-not (Ensure-EnvFile)) {
        return $false
    }
    $map = Read-EnvFile -Path $script:EnvPath
    Write-Info ('Instance root: ' + $script:Root)
    $docker = Get-DockerHealth
    Write-Info ('Docker CLI: ' + $(if ($docker.Installed) { $docker.ClientVersion } else { 'not found' }))
    Write-Info ('Docker Engine: ' + $(if ($docker.Engine) { $docker.ServerVersion } else { 'not available' }))
    Write-Info ('Compose: ' + $(if ($docker.Compose) { $docker.ComposeVersion } else { 'not available' }))
    if ($docker.Engine) {
        if (Test-InstanceOwnership -Quiet) {
            Write-Info ('Compose project owner: this checkout (' + $script:Root + ')')
        }
        else {
            Write-Fail 'Compose project owner: another checkout'
            foreach ($path in (Get-ForeignProjectPaths)) {
                Write-Info ('  foreign working directory: ' + $path)
            }
        }
    }
    Write-Info ''
    Write-Info 'Configuration (secrets masked):'
    foreach ($key in @('APP_ENV', 'PUBLIC_BASE_URL', 'BIND_ADDR', 'MODEL_MODE', 'MODEL_BASE_URL',
            'MODEL_NAME', 'MODEL_API_KEY', 'MODEL_GLOBAL_DAILY_CALLS', 'MODEL_KILL_SWITCH')) {
        $value = Get-EnvValue -Map $map -Key $key
        if ($key -match 'API_KEY|SECRET|PASSWORD|TOKEN') {
            $value = Get-MaskedValue -Value $value
        }
        if ($value -eq '') { $value = '(not set)' }
        Write-Info ('  ' + $key + ' = ' + $value)
    }
    Write-Info ''
    $state = Get-ProductState
    Write-Info ('/healthz: ' + $(if ($state.Health) { 'OK' } else { 'no response' }))
    Write-Info ('/readyz: ' + $(if ($state.Ready) { 'OK' } else { 'no response' }))
    Write-Info ('/api/v1/meta: ' + $(if ($state.Meta) { 'OK, model_mode=' + $state.ModelMode + ', version=' + $state.Version + ', commit=' + $state.Commit } else { 'no response' }))
    if ($docker.Engine -and $docker.Compose) {
        Write-Info ''
        Write-Step 'Compose status'
        Push-Location $script:Root
        try {
            & docker compose ps
        }
        finally {
            Pop-Location
        }
        Write-Info ''
        Write-Step 'Recent application logs (redacted)'
        Push-Location $script:Root
        try {
            $logs = & docker compose logs --tail=80 --no-color app 2>&1
            Write-Info (Mask-Diagnostics -Text (($logs | Out-String).Trim()) -Map $map)
        }
        finally {
            Pop-Location
        }
    }
    return $true
}

function Invoke-SelfTest {
    Write-Title 'Derive World launcher self-test'
    $failures = 0
    $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ('dw-launcher-test-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    try {
        $envFile = Join-Path $tempDir '.env'
        @(
            '# comment',
            'MODEL_MODE=real',
            'MODEL_BASE_URL=https://provider.example',
            'MODEL_NAME=example-model',
            'MODEL_API_KEY="secret-value"',
            'EMPTY='
        ) | Set-Content -LiteralPath $envFile -Encoding UTF8
        $map = Read-EnvFile -Path $envFile
        if ((Get-EnvValue -Map $map -Key 'MODEL_API_KEY') -ne 'secret-value') {
            Write-Fail 'Self-test: quoted .env values were not parsed.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: .env parsing.'
        }
        $valid = Test-ModelConfig -Map $map
        if (-not $valid.Ok -or $valid.Mode -ne 'real') {
            Write-Fail 'Self-test: a valid real configuration was rejected.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: valid real configuration accepted.'
        }
        $missingKey = @{
            MODEL_MODE = 'real'; MODEL_BASE_URL = 'https://provider.example'; MODEL_NAME = 'm'
        }
        if ((Test-ModelConfig -Map $missingKey).Ok) {
            Write-Fail 'Self-test: a real configuration without a key was accepted.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: missing API key rejected.'
        }
        $badUrl = @{
            MODEL_MODE = 'real'; MODEL_BASE_URL = 'not a url'; MODEL_NAME = 'm'; MODEL_API_KEY = 'k'
        }
        if ((Test-ModelConfig -Map $badUrl).Ok) {
            Write-Fail 'Self-test: an invalid base URL was accepted.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: invalid base URL rejected.'
        }
        $fixture = @{ MODEL_MODE = 'fixture' }
        if (-not (Test-ModelConfig -Map $fixture).Ok) {
            Write-Fail 'Self-test: fixture mode was rejected.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: fixture mode accepted without a key.'
        }
        $masked = Mask-Diagnostics -Text 'MODEL_API_KEY=secret-value' -Map $map
        if ($masked -match 'secret-value') {
            Write-Fail 'Self-test: diagnostics did not mask the API key.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: diagnostics masking.'
        }
        $masked = Mask-Diagnostics -Text 'DATABASE_URL=postgres://u:p@host/db Bearer abc.def user@example.com' -Map $map
        if ($masked -match 'postgres://' -or $masked -match 'abc\.def' -or $masked -match 'user@example\.com') {
            Write-Fail 'Self-test: diagnostics did not redact DSN, bearer token, or email.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: extended diagnostics redaction.'
        }
        if (-not (Test-SamePath 'C:\Derive World\derive-world' 'C:/Derive World/derive-world/')) {
            Write-Fail 'Self-test: equivalent Windows paths were not recognized.'
            $failures++
        }
        elseif (Test-SamePath 'C:\one' 'C:\two') {
            Write-Fail 'Self-test: different Windows paths were treated as equal.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: instance path comparison.'
        }
        $validRequired = @{
            APP_ENV = 'development'
            PUBLIC_BASE_URL = 'http://127.0.0.1:8080'
            CURSOR_HMAC_SECRET = ('x' * 32)
            IP_HASH_PEPPER = ('y' * 32)
            CAPABILITY_HMAC_SECRET = ('z' * 32)
            MODEL_MODE = 'fixture'
        }
        if (@(Test-RequiredConfig -Map $validRequired).Count -ne 0) {
            Write-Fail 'Self-test: a valid required configuration was rejected.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: required configuration accepted.'
        }
        $invalidRequired = @{ APP_ENV = 'development'; MODEL_MODE = 'fixture' }
        if (@(Test-RequiredConfig -Map $invalidRequired).Count -eq 0) {
            Write-Fail 'Self-test: an incomplete required configuration was accepted.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: missing required configuration rejected.'
        }
        $destructive = Invoke-Compose -ComposeArguments @('down', '-v')
        if ($destructive.ExitCode -ne 90) {
            Write-Fail 'Self-test: destructive Compose arguments were not refused.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: destructive Compose arguments refused.'
        }
        $maskedValue = Get-MaskedValue -Value 'secret-value'
        if ($maskedValue -match 'secret-value') {
            Write-Fail 'Self-test: masked display exposed the key.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: masked display.'
        }
        if (-not (Test-Path -LiteralPath $script:ComposePath)) {
            Write-Fail 'Self-test: compose.yaml was not found relative to the launcher.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: instance root resolution.'
        }
        if ($HealthTimeoutSeconds -lt 10) {
            Write-Fail 'Self-test: health timeout is too short.'
            $failures++
        }
        else {
            Write-Ok 'Self-test: health timeout.'
        }
    }
    finally {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    if ($failures -gt 0) {
        Write-Fail ($failures.ToString() + ' self-test check(s) failed.')
        return $false
    }
    Write-Ok 'All launcher self-tests passed.'
    return $true
}

function Show-Menu {
    while ($true) {
        Write-Title 'Derive World - local launcher'
        Write-Info '1  Start'
        Write-Info '2  Stop'
        Write-Info '3  Restart'
        Write-Info '4  Status and health'
        Write-Info '5  Open Derive World in the browser'
        Write-Info '6  AI settings / Connect AI - open configuration file'
        Write-Info '7  AI settings / Connect AI - apply and verify'
        Write-Info '8  Diagnostics (secrets redacted)'
        Write-Info '9  Environment check'
        Write-Info '10 AI configuration status'
        Write-Info '11 Switch AI to Fixture mode'
        Write-Info '0  Exit'
        $choice = Read-Host 'Choose'
        switch ($choice) {
            '1' { [void](Start-Instance) }
            '2' { [void](Stop-Instance) }
            '3' { [void](Restart-Instance) }
            '4' { [void](Show-Status) }
            '5' { $productUrl = Get-ProductUrl; Start-Process $productUrl; Write-Ok ('Opened ' + $productUrl) }
            '6' { [void](Open-AIConfig) }
            '7' { [void](Apply-AIConfig) }
            '8' { [void](Show-Diagnostics) }
            '9' { [void](Show-PrerequisiteReport) }
            '10' { Write-Title 'AI configuration status'; Show-AIConfigStatus }
            '11' { [void](Use-FixtureMode) }
            '0' { return }
            default { Write-Warn 'Choose 0-11.' }
        }
        if (-not $NoPause) {
            Write-Host ''
            Read-Host 'Press Enter to continue' | Out-Null
        }
    }
}

$exitCode = 0
try {
    $result = $true
    switch ($Command) {
        'Menu' { Show-Menu }
        'Start' { $result = Start-Instance }
        'Stop' { $result = Stop-Instance }
        'Restart' { $result = Restart-Instance }
        'Status' { $result = Show-Status }
        'Check' { $result = Show-PrerequisiteReport -AllowMissingDocker:$AllowMissingDocker }
        'Diagnostics' { $result = Show-Diagnostics }
        'OpenConfig' { $result = Open-AIConfig }
        'ApplyConfig' { $result = Apply-AIConfig }
        'StatusConfig' { Write-Title 'AI configuration status'; Show-AIConfigStatus }
        'UseFixture' { $result = Use-FixtureMode }
        'Open' { $productUrl = Get-ProductUrl; Start-Process $productUrl; Write-Ok ('Opened ' + $productUrl) }
        'SelfTest' { $result = Invoke-SelfTest }
    }
    if (-not $result) {
        $exitCode = 1
    }
}
catch {
    Write-Fail ('Launcher error: ' + $_.Exception.Message)
    Write-Info 'No data was deleted and no configuration was changed by this error.'
    $exitCode = 1
}
finally {
    if ($Command -ne 'Menu' -and -not $NoPause) {
        Write-Host ''
        Read-Host 'Press Enter to close' | Out-Null
    }
}
exit $exitCode

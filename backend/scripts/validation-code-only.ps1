param(
    [Parameter(Mandatory = $true)]
    [string]$EvidenceDirectory,
    [string]$GoExecutable = 'go',
    [switch]$SkipCompile
)
$ErrorActionPreference = 'Stop'

# A-phase never grants authority to connect, migrate, start containers or run E2E.
foreach ($name in @('SUB2API_VALIDATION_MODE', 'SUB2API_VALIDATION_CONFIG', 'SUB2API_ALLOW_DB_EXECUTION', 'SUB2API_ALLOW_LEGACY_CI_CONTAINERS', 'TEST_DATABASE_URL', 'DATABASE_URL', 'REDIS_URL', 'BASE_URL')) {
    if ([Environment]::GetEnvironmentVariable($name)) {
        throw "CODE_ONLY refuses execution/target environment variable $name."
    }
}
if (-not [IO.Path]::IsPathRooted($EvidenceDirectory) -or -not (Test-Path -LiteralPath $EvidenceDirectory -PathType Container)) {
    throw 'EvidenceDirectory must explicitly name an existing absolute candidate evidence directory.'
}
$EvidenceDirectory = (Resolve-Path -LiteralPath $EvidenceDirectory).Path
$RunDirectory = Join-Path $EvidenceDirectory ('code-only-' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff') + '-' + [Guid]::NewGuid().ToString('N'))
if (Test-Path -LiteralPath $RunDirectory) { throw 'Evidence directory collision; refusing overwrite.' }
New-Item -ItemType Directory -Path $RunDirectory | Out-Null
$Results = [Collections.Generic.List[object]]::new()

function Invoke-RecordedGo {
    param([string]$Name, [string]$Kind, [string[]]$Arguments, [string]$Artifact = '')
    $log = Join-Path $RunDirectory ($Name + '.log')
    if ((Test-Path -LiteralPath $log) -or ($Artifact -and (Test-Path -LiteralPath $Artifact))) {
        throw "Refusing to overwrite evidence for $Name."
    }
    $started = (Get-Date).ToString('o')
    & $GoExecutable @Arguments 2>&1 | Set-Content -LiteralPath $log -Encoding utf8
    $exitCode = $LASTEXITCODE
    $testCount = 0
    $skipCount = 0
    if ($Kind -eq 'CODE_ONLY' -and $exitCode -eq 0) {
        foreach ($line in Get-Content -LiteralPath $log) {
            if (-not $line.StartsWith('{')) { continue }
            $event = $line | ConvertFrom-Json
            if ($event.Test -and $event.Action -eq 'pass') { $testCount++ }
            if ($event.Test -and $event.Action -eq 'skip') { $skipCount++ }
        }
        if ($testCount -eq 0) { $exitCode = 1 }
    }
    $hash = $null
    if ($Artifact -and $exitCode -eq 0) {
        if (-not (Test-Path -LiteralPath $Artifact -PathType Leaf)) { $exitCode = 1 }
        else { $hash = (Get-FileHash -LiteralPath $Artifact -Algorithm SHA256).Hash.ToLowerInvariant() }
    }
    $status = if ($exitCode -ne 0) { 'FAIL' } elseif ($Kind -eq 'CODE_ONLY') { 'PASS' } else { 'COMPILED_NOT_RUN' }
    $Results.Add([pscustomobject]@{ name=$Name; kind=$Kind; status=$status; cwd=(Get-Location).Path; command=@($GoExecutable)+$Arguments; started=$started; ended=(Get-Date).ToString('o'); exit_code=$exitCode; passed_test_events=$testCount; skipped_test_events=$skipCount; log=$log; artifact=$Artifact; sha256=$hash })
    $Results | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $RunDirectory 'results.json') -Encoding utf8
    if ($exitCode -ne 0) { throw "$Name failed (exit=$exitCode or missing/non-empty result). Evidence: $log" }
}

Push-Location (Join-Path $PSScriptRoot '..')
try {
    Invoke-RecordedGo -Name 'validation-unit' -Kind 'CODE_ONLY' -Arguments @('test','-mod=readonly','-p=2','-json','./internal/repository/validation','-count=1')
    Invoke-RecordedGo -Name 'migration-static-unit' -Kind 'CODE_ONLY' -Arguments @('test','-mod=readonly','-p=2','-json','./migrations','-count=1')
    if (-not $SkipCompile) {
        foreach ($entry in @(
            @{name='repository-integration'; tag='integration'; package='./internal/repository'},
            @{name='securityaudit-integration'; tag='integration'; package='./internal/securityaudit'},
            @{name='service-integration'; tag='integration'; package='./internal/service'},
            @{name='migrations-integration'; tag='integration'; package='./migrations'},
            @{name='application-e2e'; tag='e2e'; package='./internal/integration'}
        )) {
            $binary = Join-Path $RunDirectory ($entry.name + '.test.exe')
            Invoke-RecordedGo -Name $entry.name -Kind 'COMPILE_ONLY' -Arguments @('test','-mod=readonly','-p=2','-c',('-tags='+$entry.tag),'-o',$binary,$entry.package) -Artifact $binary
        }
    }
    if ($Results.Count -eq 0) { throw 'Empty execution set is not a passing gate.' }
    Write-Output "CODE_ONLY evidence: $RunDirectory"
} finally {
    Pop-Location
}

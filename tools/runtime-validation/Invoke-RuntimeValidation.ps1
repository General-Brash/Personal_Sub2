#requires -Version 7.2
<##
.SYNOPSIS
    为 Personal_Sub2 178-P1 + 官方 179-183 融合工作树执行隔离运行时验证。
.DESCRIPTION
    默认只读检查；临时构建、Compose env、容器卷和日志全部写入系统 TEMP。
    传入 -RunDockerRuntime 才启动临时 Compose 项目；不执行发布、push 或部署。
    退出码：0=无 FAIL/BLOCKED；1=有 FAIL；2=无 FAIL 但有 BLOCKED。
#>
[CmdletBinding()]
param(
    [string]$RepoRoot = '',
    [switch]$RunDockerRuntime,
    [switch]$BuildDockerImages,
    [string]$AppBaseUrl = '',
    [string]$ClassifierBaseUrl = '',
    [string]$ClassifierModelDir = '',
    [string]$ClassifierModelVersion = '',
    [string]$AppBearerTokenEnvName = '',
    [string]$ClassifierApiTokenEnvName = 'INTENT_CLASSIFIER_API_TOKEN',
    [int]$TimeoutSeconds = 300
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($RepoRoot)) { $RepoRoot = (Resolve-Path (Join-Path $ScriptRoot '..\..')).Path }
$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path
$DeployRoot = Join-Path $RepoRoot 'deploy'
$BackendRoot = Join-Path $RepoRoot 'backend'
$FrontendRoot = Join-Path $RepoRoot 'frontend'
$RunId = 'rv-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + ([guid]::NewGuid().ToString('N').Substring(0, 8))
$RunRoot = Join-Path ([IO.Path]::GetTempPath()) ('sub2api-runtime-validation\' + $RunId)
$EvidenceRoot = Join-Path $RunRoot 'evidence'
New-Item -ItemType Directory -Path $EvidenceRoot -Force | Out-Null
$Results = [System.Collections.Generic.List[object]]::new()
$Secrets = [System.Collections.Generic.List[string]]::new()
$CommandCounter = 0
$DockerDaemonAvailable = $false
$DockerPath = $null
$GoPath = $null
$ComposeEnvFile = $null
$ProjectName = ('sub2api-' + $RunId.ToLowerInvariant()).Replace('-', '')
$ServerPort = $null
$ClassifierPort = $null
$ModelDirForCompose = $null
$RuntimeComposeFile = $null
$RuntimeOverrideFile = $null

function Add-Secret {
    param([AllowNull()][string]$Value)
    if (-not [string]::IsNullOrWhiteSpace($Value) -and $Value.Length -ge 4 -and -not $Secrets.Contains($Value)) { [void]$Secrets.Add($Value) }
}
function New-RandomSecret {
    $bytes = New-Object byte[] 24
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    return [Convert]::ToHexString($bytes).ToLowerInvariant()
}
function ConvertTo-RedactedText {
    param([AllowNull()][string]$Text)
    if ($null -eq $Text) { return '' }
    $result = $Text
    foreach ($secret in $Secrets) { $result = $result.Replace($secret, '<redacted>') }
    return [regex]::Replace($result, '(?im)(\b(?:password|token|secret|api[_-]?key|authorization)\b\s*[:=]\s*)([^\s,;"''}]+)', '$1<redacted>')
}
function Get-Slug {
    param([string]$Value)
    $slug = ($Value -replace '[^A-Za-z0-9._-]+', '_').Trim('_')
    if ([string]::IsNullOrWhiteSpace($slug)) { $slug = 'command' }
    return $slug.Substring(0, [Math]::Min(90, $slug.Length))
}
function Get-SafeCommandLine {
    param([string]$FilePath, [string[]]$Arguments)
    $safe = foreach ($argument in $Arguments) { if ($argument -match '(?i)(password|token|secret|api[_-]?key|authorization)') { '<redacted-arg>' } else { $argument } }
    return (($FilePath, $safe) -join ' ')
}
function Invoke-External {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [string[]]$Arguments = @(),
        [string]$WorkingDirectory = $RepoRoot,
        [hashtable]$EnvironmentOverrides = @{},
        [string]$InputText = '',
        [int]$Timeout = $TimeoutSeconds,
        [Parameter(Mandatory)][string]$Name
    )
    $script:CommandCounter++
    $slug = '{0:D3}-{1}' -f $script:CommandCounter, (Get-Slug $Name)
    $safeCommand = Get-SafeCommandLine -FilePath $FilePath -Arguments $Arguments
    [IO.File]::WriteAllText((Join-Path $EvidenceRoot ($slug + '.command.txt')), $safeCommand + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
    $psi = [Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $FilePath; $psi.WorkingDirectory = $WorkingDirectory; $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true; $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    if ($psi.PSObject.Properties.Name -contains 'ArgumentList') { foreach ($argument in $Arguments) { [void]$psi.ArgumentList.Add($argument) } } else { $psi.Arguments = (($Arguments | ForEach-Object { if ($_ -match '[\s"]') { '"' + ($_ -replace '(\\*)"', '$1$1\"' -replace '(\\+)$', '$1$1') + '"' } else { $_ } }) -join ' ') }
    foreach ($key in $EnvironmentOverrides.Keys) { $psi.EnvironmentVariables[$key] = [string]$EnvironmentOverrides[$key] }
    $process = [Diagnostics.Process]::new(); $process.StartInfo = $psi; $started = Get-Date
    try {
        if (-not $process.Start()) { throw ('无法启动进程: ' + $FilePath) }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync(); $stderrTask = $process.StandardError.ReadToEndAsync()
        if (-not [string]::IsNullOrEmpty($InputText)) { $process.StandardInput.Write($InputText); $process.StandardInput.Close() }
        $completed = $process.WaitForExit([Math]::Max(1, $Timeout) * 1000); $timedOut = -not $completed
        if ($timedOut) { try { $process.Kill($true) } catch { try { $process.Kill() } catch {} }; $process.WaitForExit() }
        $stdout = $stdoutTask.GetAwaiter().GetResult(); $stderr = $stderrTask.GetAwaiter().GetResult(); $exitCode = if ($timedOut) { 124 } else { $process.ExitCode }
    } catch { $stdout = ''; $stderr = $_.Exception.ToString(); $exitCode = 127; $timedOut = $false } finally { $durationMs = [int]((Get-Date) - $started).TotalMilliseconds; $process.Dispose() }
    $safeOutput = ConvertTo-RedactedText (($stdout + [Environment]::NewLine + $stderr).TrimEnd())
    $evidence = Join-Path $EvidenceRoot ($slug + '.log')
    [IO.File]::WriteAllText($evidence, $safeOutput + [Environment]::NewLine, [Text.UTF8Encoding]::new($false))
    return [pscustomobject]@{ Name=$Name; CommandLine=$safeCommand; Stdout=$stdout; Stderr=$stderr; SafeOutput=$safeOutput; ExitCode=$exitCode; TimedOut=$timedOut; DurationMs=$durationMs; Evidence=$evidence }
}
function Add-Result {
    param([Parameter(Mandatory)][string]$Name,[Parameter(Mandatory)][ValidateSet('PASS','FAIL','BLOCKED','NOT_RUN')][string]$Status,[Parameter(Mandatory)][string]$Summary,[string]$Command='', [string]$Evidence='')
    [void]$Results.Add([pscustomobject]@{Name=$Name;Status=$Status;Summary=$Summary;Command=(ConvertTo-RedactedText $Command);Evidence=$Evidence})
    Write-Host ('[{0}] {1}: {2}' -f $Status,$Name,$Summary)
}
function Add-CommandResult {
    param([Parameter(Mandatory)]$Outcome,[Parameter(Mandatory)][string]$Name,[string]$PassSummary='命令执行成功',[string]$FailSummary='命令执行失败',[int[]]$AllowedExitCodes=@(0))
    if ($Outcome.TimedOut) { Add-Result -Name $Name -Status FAIL -Summary ($FailSummary + '（超时）') -Command $Outcome.CommandLine -Evidence $Outcome.Evidence }
    elseif ($AllowedExitCodes -contains $Outcome.ExitCode) { Add-Result -Name $Name -Status PASS -Summary $PassSummary -Command $Outcome.CommandLine -Evidence $Outcome.Evidence }
    else { $detail=($Outcome.SafeOutput -split '\r?\n'|Where-Object{$_.Trim()}|Select-Object -Last 1);if([string]::IsNullOrWhiteSpace($detail)){$detail='无额外输出'};Add-Result -Name $Name -Status FAIL -Summary ($FailSummary + ': ' + $detail) -Command $Outcome.CommandLine -Evidence $Outcome.Evidence }
}
function Resolve-ToolPath { param([Parameter(Mandatory)][string]$Name);$command=Get-Command $Name -ErrorAction SilentlyContinue;if($null -eq $command){return $null};return $command.Source }
function Resolve-BashPath {
    $roots=@($env:ProgramFiles,${env:ProgramFiles(x86)},$env:LOCALAPPDATA)|Where-Object{ -not [string]::IsNullOrWhiteSpace($_) }|Select-Object -Unique
    foreach($root in $roots){
        foreach($relative in @('Git\bin\bash.exe','Git\usr\bin\bash.exe','Programs\Git\bin\bash.exe','Programs\Git\usr\bin\bash.exe')){
            $candidate=Join-Path $root $relative
            if(Test-Path -LiteralPath $candidate -PathType Leaf){return(Resolve-Path -LiteralPath $candidate).Path}
        }
    }
    foreach($command in @(Get-Command bash.exe -All -ErrorAction SilentlyContinue)){
        $candidate=$command.Source
        if([string]::IsNullOrWhiteSpace($candidate)){$candidate=$command.Path}
        if([string]::IsNullOrWhiteSpace($candidate)){continue}
        if($candidate -match '(?i)[\/](?:Windows[\/]System32|WindowsApps)[\/]bash\.exe$'){continue}
        if(Test-Path -LiteralPath $candidate -PathType Leaf){return(Resolve-Path -LiteralPath $candidate).Path}
    }
    return $null
}
function Resolve-GoPath { $cached=Join-Path $env:USERPROFILE 'go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.0.windows-amd64\bin\go.exe';if(Test-Path -LiteralPath $cached){return $cached};return Resolve-ToolPath 'go' }
function Get-FreeTcpPort { $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0);try{$listener.Start();return([Net.IPEndPoint]$listener.LocalEndpoint).Port}finally{$listener.Stop()} }

function New-ComposeEnvironment {
    $script:ServerPort = Get-FreeTcpPort
    do { $script:ClassifierPort = Get-FreeTcpPort } while ($ClassifierPort -eq $ServerPort)
    $script:ModelDirForCompose = if ([string]::IsNullOrWhiteSpace($ClassifierModelDir)) { Join-Path $RunRoot 'empty-models' } else { (Resolve-Path -LiteralPath $ClassifierModelDir).Path }
    New-Item -ItemType Directory -Path $ModelDirForCompose -Force | Out-Null
    $composeModelPath = $ModelDirForCompose.Replace('\', '/')
    $values = [ordered]@{
        BIND_HOST='127.0.0.1'; SERVER_PORT=[string]$ServerPort; POSTGRES_USER='rv_sub2api'; POSTGRES_PASSWORD=(New-RandomSecret); POSTGRES_DB='rv_sub2api'; DATABASE_HOST='127.0.0.1'; DATABASE_PORT='5432'; REDIS_HOST='127.0.0.1'; REDIS_PORT='6379'; REDIS_PASSWORD=(New-RandomSecret); ADMIN_EMAIL='runtime-validation@example.invalid'; ADMIN_PASSWORD=(New-RandomSecret); JWT_SECRET=(New-RandomSecret); ENCRYPTION_KEY=(New-RandomSecret); PAYMENT_RESUME_SIGNING_KEY=(New-RandomSecret); INTENT_CLASSIFIER_ADMIN_TOKEN=(New-RandomSecret); INTENT_CLASSIFIER_API_TOKEN=(New-RandomSecret); INTENT_CLASSIFIER_MODEL_DIR=$composeModelPath; INTENT_CLASSIFIER_ACTIVE_VERSION=$ClassifierModelVersion; INTENT_CLASSIFIER_MODEL_VERSION=$ClassifierModelVersion; INTENT_CLASSIFIER_MAX_CONCURRENCY='2'; INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS='500'; SUB2API_IMAGE='ghcr.io/general-brash/personal_sub2:latest'; INTENT_CLASSIFIER_IMAGE='ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.183-P1'; DATABASE_SSLMODE='disable'; RUN_MODE='standard'; SERVER_MODE='release'; TZ='Asia/Shanghai'; RUNTIME_SERVER_PORT=[string]$ServerPort; RUNTIME_CLASSIFIER_PORT=[string]$ClassifierPort
    }
    $values.DATABASE_USER = 'rv_sub2api'
    $values.DATABASE_PASSWORD = $values.POSTGRES_PASSWORD
    $values.DATABASE_DBNAME = $values.POSTGRES_DB
    foreach ($key in $values.Keys) { Add-Secret ([string]$values[$key]) }
    $envPath = Join-Path $RunRoot 'compose.env'
    $envLines = foreach ($key in $values.Keys) { '{0}={1}' -f $key, $values[$key] }
    [IO.File]::WriteAllLines($envPath, $envLines, [Text.UTF8Encoding]::new($false))
    $script:ComposeEnvFile = $envPath
    return $values
}

function Run-GoTest {
    param([Parameter(Mandatory)][string]$Name,[Parameter(Mandatory)][string[]]$Packages,[Parameter(Mandatory)][string]$Regex,[ValidateSet('unit','integration','e2e')][string]$Tags='unit',[int]$Timeout=300)
    if ($null -eq $GoPath) { Add-Result -Name $Name -Status BLOCKED -Summary '未找到 Go 工具链'; return }
    $args = @('test','-mod=readonly',('-tags=' + $Tags)) + $Packages + @('-run',$Regex,'-count=1')
    $outcome = Invoke-External -FilePath $GoPath -Arguments $args -WorkingDirectory $BackendRoot -EnvironmentOverrides @{GOTOOLCHAIN='local';CGO_ENABLED='0';GOFLAGS='-mod=readonly'} -Timeout $Timeout -Name $Name
    Add-CommandResult -Outcome $outcome -Name $Name -PassSummary 'Go 测试通过' -FailSummary 'Go 测试失败'
}
function Get-TestFunctionNames {
    param([Parameter(Mandatory)][string]$Root)
    $names=[System.Collections.Generic.List[string]]::new()
    foreach($file in(Get-ChildItem -LiteralPath $Root -Recurse -File -Filter '*_test.go' -ErrorAction SilentlyContinue)){
        foreach($match in(Select-String -LiteralPath $file.FullName -Pattern '^func (Test[A-Za-z0-9_]+)' -AllMatches)){
            foreach($capture in $match.Matches){[void]$names.Add($capture.Groups[1].Value)}
        }
    }
    return $names.ToArray()
}
function Run-OptionalGoContract {
    param([Parameter(Mandatory)][string]$Name,[Parameter(Mandatory)][string]$SearchRoot,[Parameter(Mandatory)][string]$NameRegex,[Parameter(Mandatory)][string]$Package,[ValidateSet('unit','integration','e2e')][string]$Tags='unit',[int]$Timeout=300)
    $names=@(Get-TestFunctionNames -Root $SearchRoot|Where-Object{$_ -match $NameRegex})
    if($names.Count -eq 0){Add-Result -Name $Name -Status NOT_RUN -Summary ('当前代码没有匹配的直接测试入口；匹配式: '+$NameRegex);return}
    $regex='^(?:'+(($names|ForEach-Object{[regex]::Escape($_)}) -join '|')+')$'
    Run-GoTest -Name $Name -Packages @($Package) -Regex $regex -Tags $Tags -Timeout $Timeout
}
function Run-FrontendChecks {
    $node=Resolve-ToolPath 'node'
    if($null -eq $node){Add-Result -Name '前端现有 node_modules 检查' -Status BLOCKED -Summary '未找到 node';return $null}
    $vueTsc=Join-Path $FrontendRoot 'node_modules\vue-tsc\bin\vue-tsc.js';$eslint=Join-Path $FrontendRoot 'node_modules\eslint\bin\eslint.js';$vitest=Join-Path $FrontendRoot 'node_modules\vitest\vitest.mjs';$vite=Join-Path $FrontendRoot 'node_modules\vite\bin\vite.js'
    $missing=@(@($vueTsc,$eslint,$vitest,$vite)|Where-Object{-not(Test-Path -LiteralPath $_)})
    if($missing.Count -gt 0){Add-Result -Name '前端现有 node_modules 检查' -Status BLOCKED -Summary ('缺少本地工具: '+(($missing|ForEach-Object{Split-Path $_ -Leaf}) -join ', '));return $null}
    Add-Result -Name '前端现有 node_modules 检查' -Status PASS -Summary '直接复用本地 node_modules；未运行 pnpm install'
    $typecheck=Invoke-External -FilePath $node -Arguments @($vueTsc,'--noEmit','--pretty','false') -WorkingDirectory $FrontendRoot -Timeout 300 -Name 'frontend-typecheck';Add-CommandResult -Outcome $typecheck -Name '前端 TypeScript 类型检查' -PassSummary 'vue-tsc 通过' -FailSummary 'vue-tsc 失败'
    $lint=Invoke-External -FilePath $node -Arguments @($eslint,'.','--ext','.vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts') -WorkingDirectory $FrontendRoot -Timeout 300 -Name 'frontend-eslint';Add-CommandResult -Outcome $lint -Name '前端 ESLint 检查' -PassSummary 'ESLint 通过' -FailSummary 'ESLint 失败'
    $makefile=Join-Path $RepoRoot 'Makefile';$specs=@();if(Test-Path -LiteralPath $makefile){$specs=@(Get-Content -LiteralPath $makefile|ForEach-Object{$line=$_.Trim();$line=$line.TrimEnd('\').Trim();if($line -match '^src/.+\.spec\.ts$'){$line}})}
    $missingSpecs=@($specs|Where-Object{-not(Test-Path -LiteralPath(Join-Path $FrontendRoot $_))})
    if($specs.Count -eq 0){Add-Result -Name '前端 critical spec 清单' -Status FAIL -Summary '根 Makefile 未解析出 spec 路径'}elseif($missingSpecs.Count -gt 0){Add-Result -Name '前端 critical spec 清单' -Status FAIL -Summary ('缺少 '+$missingSpecs.Count+' 个 spec')}else{Add-Result -Name '前端 critical spec 清单' -Status PASS -Summary ($specs.Count.ToString()+' 个 spec 路径全部存在');$tests=Invoke-External -FilePath $node -Arguments(@($vitest,'run')+$specs) -WorkingDirectory $FrontendRoot -Timeout 600 -Name 'frontend-critical-vitest';Add-CommandResult -Outcome $tests -Name '前端 critical Vitest' -PassSummary ('critical spec 执行通过（'+$specs.Count+' 个文件）') -FailSummary 'critical Vitest 失败'}
    $frontendDist=Join-Path $RunRoot 'frontend-dist';New-Item -ItemType Directory -Path $frontendDist -Force|Out-Null;$build=Invoke-External -FilePath $node -Arguments @($vite,'build','--outDir',$frontendDist,'--emptyOutDir') -WorkingDirectory $FrontendRoot -Timeout 600 -Name 'frontend-vite-build-isolated'
    if($build.ExitCode -eq 0 -and -not $build.TimedOut -and(Test-Path -LiteralPath(Join-Path $frontendDist 'index.html'))){Add-Result -Name '前端隔离生产构建' -Status PASS -Summary 'Vite 输出到临时目录，index.html 存在' -Command $build.CommandLine -Evidence $build.Evidence}elseif($build.ExitCode -eq 0 -and -not $build.TimedOut){Add-Result -Name '前端隔离生产构建' -Status FAIL -Summary 'Vite 成功但临时输出缺少 index.html' -Command $build.CommandLine -Evidence $build.Evidence}else{Add-Result -Name '前端隔离生产构建' -Status FAIL -Summary 'Vite 隔离构建失败' -Command $build.CommandLine -Evidence $build.Evidence}
    return $frontendDist
}

function Run-EmbeddedBackendBuild {
    param([AllowNull()][string]$FrontendDist)
    if($null -eq $GoPath){Add-Result -Name 'Go embedded frontend 构建' -Status BLOCKED -Summary '未找到 Go 工具链';return}
    if([string]::IsNullOrWhiteSpace($FrontendDist) -or -not(Test-Path -LiteralPath(Join-Path $FrontendDist 'index.html'))){Add-Result -Name 'Go embedded frontend 构建' -Status BLOCKED -Summary '隔离前端构建没有生成可嵌入的 index.html';return}
    $stageBackend=Join-Path $RunRoot 'backend-stage';$robocopy=Resolve-ToolPath 'robocopy.exe';if($null -eq $robocopy){Add-Result -Name 'Go embedded frontend 构建' -Status BLOCKED -Summary '未找到 robocopy，无法建立后端隔离暂存副本';return}
    $copy=Invoke-External -FilePath $robocopy -Arguments @($BackendRoot,$stageBackend,'/E','/XJ','/R:0','/W:0','/NFL','/NDL','/NP','/XD','bin','dist','tmp') -WorkingDirectory $RepoRoot -Timeout 300 -Name 'backend-stage-copy';if($copy.ExitCode -gt 7 -or $copy.TimedOut){Add-Result -Name 'Go embedded frontend 构建' -Status FAIL -Summary '建立后端隔离暂存副本失败' -Command $copy.CommandLine -Evidence $copy.Evidence;return}
    $embeddedDist=Join-Path $stageBackend 'internal\web\dist';New-Item -ItemType Directory -Path $embeddedDist -Force|Out-Null
    $frontendEntries=@(Get-ChildItem -LiteralPath $FrontendDist -Force);if($frontendEntries.Count -eq 0){Add-Result -Name 'Go embedded frontend 构建' -Status FAIL -Summary '隔离前端输出目录为空，拒绝继续复制';return};foreach($entry in $frontendEntries){Copy-Item -LiteralPath $entry.FullName -Destination (Join-Path $embeddedDist $entry.Name) -Recurse -Force}
    $embedTest=Invoke-External -FilePath $GoPath -Arguments @('test','-mod=readonly','./internal/web','-run','^$','-count=1') -WorkingDirectory $stageBackend -EnvironmentOverrides @{GOTOOLCHAIN='local';CGO_ENABLED='0';GOFLAGS='-mod=readonly'} -Timeout 300 -Name 'embedded-frontend-package-compile';if($embedTest.ExitCode -ne 0 -or $embedTest.TimedOut){Add-Result -Name 'Go embedded frontend 包编译' -Status FAIL -Summary 'internal/web 编译失败' -Command $embedTest.CommandLine -Evidence $embedTest.Evidence;return}
    $binary=Join-Path $RunRoot 'sub2api-server.exe';$build=Invoke-External -FilePath $GoPath -Arguments @('build','-mod=readonly','-trimpath','-o',$binary,'./cmd/server') -WorkingDirectory $stageBackend -EnvironmentOverrides @{GOTOOLCHAIN='local';CGO_ENABLED='0';GOFLAGS='-mod=readonly'} -Timeout 600 -Name 'backend-embedded-build';if($build.ExitCode -eq 0 -and -not $build.TimedOut -and(Test-Path -LiteralPath $binary)){Add-Result -Name 'Go embedded frontend 构建' -Status PASS -Summary '当前后端源码与临时前端产物完成 CGO_ENABLED=0 隔离构建；二进制在 TEMP' -Command $build.CommandLine -Evidence $build.Evidence}elseif($build.ExitCode -eq 0 -and -not $build.TimedOut){Add-Result -Name 'Go embedded frontend 构建' -Status FAIL -Summary 'Go build 成功但临时二进制不存在' -Command $build.CommandLine -Evidence $build.Evidence}else{Add-Result -Name 'Go embedded frontend 构建' -Status FAIL -Summary 'cmd/server 隔离构建失败' -Command $build.CommandLine -Evidence $build.Evidence}
}
function Run-ClassifierLocalTests {
    $python=Resolve-ToolPath 'python';if($null -eq $python){Add-Result -Name 'Intent Classifier 本地 API 测试' -Status BLOCKED -Summary '未找到 python';return}
    $serviceRoot=Join-Path $RepoRoot 'services\intent-classifier';$testFile=Join-Path $serviceRoot 'tests\test_api.py';if(-not(Test-Path -LiteralPath $testFile)){Add-Result -Name 'Intent Classifier 本地 API 测试' -Status BLOCKED -Summary '缺少 test_api.py';return}
    $pytestBase=Join-Path $RunRoot 'pytest-basetemp';New-Item -ItemType Directory -Path $pytestBase -Force|Out-Null
    $outcome=Invoke-External -FilePath $python -Arguments @('-m','pytest','-q','-p','no:cacheprovider','--basetemp',$pytestBase,'tests\test_api.py') -WorkingDirectory $serviceRoot -EnvironmentOverrides @{PYTHONPATH=(Join-Path $serviceRoot 'src');PYTHONDONTWRITEBYTECODE='1';PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'} -Timeout 600 -Name 'intent-classifier-local-api-tests'
    if($outcome.ExitCode -eq 0 -and -not $outcome.TimedOut){Add-Result -Name 'Intent Classifier 本地 API 测试' -Status PASS -Summary 'TestClient 已执行 live/ready 和带 Bearer Token 的 classify 请求；无仓库内 pytest cache' -Command $outcome.CommandLine -Evidence $outcome.Evidence}elseif($outcome.SafeOutput -match '(?i)(ModuleNotFoundError|No module named|ImportError)'){Add-Result -Name 'Intent Classifier 本地 API 测试' -Status BLOCKED -Summary 'Python 依赖未就绪，未把依赖缺失误报为代码失败' -Command $outcome.CommandLine -Evidence $outcome.Evidence}else{Add-Result -Name 'Intent Classifier 本地 API 测试' -Status FAIL -Summary 'Classifier 本地 API 测试失败' -Command $outcome.CommandLine -Evidence $outcome.Evidence}
}

function Run-ExistingShellContracts {
    $bash=Resolve-BashPath;if($null -eq $bash){Add-Result -Name '现有 Bash 部署合同测试' -Status NOT_RUN -Summary '当前 Windows 环境未找到 bash；没有改用不等价的 shell 模拟';return}
    foreach($name in @('docker-compose-gateway-env-test.sh','docker-compose-security-test.sh','docker-doc-contract-test.sh','docker-runtime-resources-test.sh','intent-classifier-deploy-test.sh')){
        $path=Join-Path $DeployRoot ('tests\'+$name)
        if(-not(Test-Path -LiteralPath $path)){Add-Result -Name ('现有合同: '+$name) -Status NOT_RUN -Summary '脚本不存在';continue}
        $outcome=Invoke-External -FilePath $bash -Arguments @($path) -WorkingDirectory $RepoRoot -Timeout 180 -Name ('shell-'+$name)
        if($outcome.ExitCode -ne 0 -and $outcome.SafeOutput -match '(?i)(WSL|execvpe|No such file or directory|cannot find.*bash)'){
            Add-Result -Name ('现有合同: '+$name) -Status BLOCKED -Summary 'bash 命令存在但其 WSL/Linux 运行时不可用；未把环境阻断误报为代码失败' -Command $outcome.CommandLine -Evidence $outcome.Evidence
        }else{
            Add-CommandResult -Outcome $outcome -Name ('现有合同: '+$name) -PassSummary '现有 Bash 合同测试通过' -FailSummary '现有 Bash 合同测试失败'
        }
    }
}
function Invoke-HttpProbe {
    param([Parameter(Mandatory)][string]$Name,[Parameter(Mandatory)][string]$Uri,[ValidateSet('GET','POST')][string]$Method='GET',[hashtable]$Headers=@{},[string]$JsonBody='', [int[]]$ExpectedStatus=@(200),[int]$Timeout=15)
    $handler=[Net.Http.HttpClientHandler]::new();$handler.UseProxy=$false;$client=[Net.Http.HttpClient]::new($handler);$client.Timeout=[TimeSpan]::FromSeconds($Timeout);$request=[Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::$Method,$Uri)
    foreach($key in $Headers.Keys){Add-Secret ([string]$Headers[$key]);[void]$request.Headers.TryAddWithoutValidation($key,[string]$Headers[$key])};if(-not[string]::IsNullOrWhiteSpace($JsonBody)){$request.Content=[Net.Http.StringContent]::new($JsonBody,[Text.Encoding]::UTF8,'application/json')}
    try{$response=$client.SendAsync($request).GetAwaiter().GetResult();$body=$response.Content.ReadAsStringAsync().GetAwaiter().GetResult();$status=[int]$response.StatusCode;$matched=$ExpectedStatus -contains $status;$evidence=Join-Path $EvidenceRoot ((Get-Slug $Name)+'-'+$CommandCounter.ToString('D3')+'.http.log');[IO.File]::WriteAllText($evidence,('HTTP {0} {1}' -f $status,$Uri)+[Environment]::NewLine+(ConvertTo-RedactedText $body)+[Environment]::NewLine,[Text.UTF8Encoding]::new($false));if($matched){Add-Result -Name $Name -Status PASS -Summary ('HTTP '+$status) -Command ($Method+' '+$Uri) -Evidence $evidence}else{Add-Result -Name $Name -Status FAIL -Summary ('HTTP '+$status+'，期望 '+(($ExpectedStatus|ForEach-Object{$_}) -join '/')) -Command ($Method+' '+$Uri) -Evidence $evidence};return [pscustomobject]@{StatusCode=$status;Body=$body;Matched=$matched;Evidence=$evidence}}catch{$evidence=Join-Path $EvidenceRoot ((Get-Slug $Name)+'-'+$CommandCounter.ToString('D3')+'.http.log');[IO.File]::WriteAllText($evidence,(ConvertTo-RedactedText $_.Exception.ToString())+[Environment]::NewLine,[Text.UTF8Encoding]::new($false));Add-Result -Name $Name -Status FAIL -Summary 'HTTP 请求异常' -Command ($Method+' '+$Uri) -Evidence $evidence;return [pscustomobject]@{StatusCode=0;Body='';Matched=$false;Evidence=$evidence}}finally{$request.Dispose();$client.Dispose();$handler.Dispose()}
}

function Run-ApplicationHttpChecks {
    param([Parameter(Mandatory)][string]$BaseUrl)
    $base=$BaseUrl.TrimEnd('/');[void](Invoke-HttpProbe -Name 'Sub2API /health' -Uri ($base+'/health'));[void](Invoke-HttpProbe -Name 'Sub2API embedded frontend 根页面' -Uri ($base+'/'))
    $pluginToken=$null;if(-not[string]::IsNullOrWhiteSpace($AppBearerTokenEnvName)){$pluginToken=[Environment]::GetEnvironmentVariable($AppBearerTokenEnvName);Add-Secret $pluginToken}
    if([string]::IsNullOrWhiteSpace($pluginToken)){[void](Invoke-HttpProbe -Name 'Plugin admin route 未认证保护' -Uri ($base+'/api/v1/admin/plugins') -ExpectedStatus @(401,403));Add-Result -Name 'Plugin authenticated real request' -Status NOT_RUN -Summary '未提供 admin Bearer Token 和 plugin fixture；只验证了未认证路由保护'}else{$plugins=Invoke-HttpProbe -Name 'Plugin admin list authenticated request' -Uri ($base+'/api/v1/admin/plugins') -Headers @{Authorization=('Bearer '+$pluginToken)};if($plugins.Matched){Add-Result -Name 'Plugin authenticated real request' -Status PASS -Summary '已通过真实 HTTP admin list 请求；未执行上传/启停等有副作用操作' -Command ('GET '+$base+'/api/v1/admin/plugins') -Evidence $plugins.Evidence}}
}

function Run-ClassifierHttpChecks {
    param([Parameter(Mandatory)][string]$BaseUrl)
    $base=$BaseUrl.TrimEnd('/');[void](Invoke-HttpProbe -Name 'Intent Classifier /health/live' -Uri ($base+'/health/live'))
    if([string]::IsNullOrWhiteSpace($ClassifierModelVersion) -and [string]::IsNullOrWhiteSpace($ClassifierModelDir)){Add-Result -Name 'Intent Classifier /health/ready' -Status NOT_RUN -Summary '未提供模型目录/版本；只验证 live，不把无模型 503 当作通过';Add-Result -Name 'Intent Classifier /v1/classify real request' -Status NOT_RUN -Summary '未提供可加载模型版本和 API Token';return}
    $ready=Invoke-HttpProbe -Name 'Intent Classifier /health/ready' -Uri ($base+'/health/ready');if(-not $ready.Matched){return}
    $apiToken=[Environment]::GetEnvironmentVariable($ClassifierApiTokenEnvName);Add-Secret $apiToken;if([string]::IsNullOrWhiteSpace($apiToken)){Add-Result -Name 'Intent Classifier /v1/classify real request' -Status NOT_RUN -Summary ('环境变量 '+$ClassifierApiTokenEnvName+' 未提供');return}
    $payload=@{schema_version='1';request_id='runtime-validation-request';text='safe runtime validation request';matched_keyword='scan';context=@{protocol='openai_chat';endpoint='/v1/chat/completions';model='gpt-test'}}|ConvertTo-Json -Depth 4 -Compress
    $response=Invoke-HttpProbe -Name 'Intent Classifier /v1/classify real request' -Uri ($base+'/v1/classify') -Method POST -Headers @{Authorization=('Bearer '+$apiToken)} -JsonBody $payload
    if($response.Matched){try{$body=$response.Body|ConvertFrom-Json;$required=@('schema_version','label','score','model_version','trace_id');$missing=@($required|Where-Object{$null -eq $body.$_});if($missing.Count -gt 0){Add-Result -Name 'Intent Classifier response contract' -Status FAIL -Summary ('缺少字段: '+($missing -join ', ')) -Evidence $response.Evidence}else{Add-Result -Name 'Intent Classifier response contract' -Status PASS -Summary '真实 classify 响应包含五个适配器合同字段' -Evidence $response.Evidence}}catch{Add-Result -Name 'Intent Classifier response contract' -Status FAIL -Summary 'classify 响应不是合法 JSON' -Evidence $response.Evidence}}
}
function Run-ComposeConfigChecks {
    if($null -eq $DockerPath){Add-Result -Name 'Compose 默认/tag/digest config' -Status BLOCKED -Summary '未找到 Docker CLI';return}
    $files=@(Get-ChildItem -LiteralPath $DeployRoot -File -Filter 'docker-compose*.yml'|Sort-Object Name)
    $modes=@(@{Name='default';Image='ghcr.io/general-brash/personal_sub2:latest'},@{Name='tag';Image='ghcr.io/general-brash/personal_sub2:v0.1.183-P1'},@{Name='digest';Image='ghcr.io/general-brash/personal_sub2@sha256:1111111111111111111111111111111111111111111111111111111111111111'})
    foreach($file in $files){foreach($mode in $modes){$args=@('compose','--env-file',$ComposeEnvFile,'-f',$file.FullName,'-p',$ProjectName,'config','--format','json');$outcome=Invoke-External -FilePath $DockerPath -Arguments $args -WorkingDirectory $RepoRoot -EnvironmentOverrides @{SUB2API_IMAGE=$mode.Image} -Timeout 90 -Name ('compose-config-'+$file.BaseName+'-'+$mode.Name);if($outcome.ExitCode -ne 0 -or $outcome.TimedOut){Add-Result -Name ('Compose config '+$file.Name+' ['+$mode.Name+']') -Status FAIL -Summary 'docker compose config 失败' -Command $outcome.CommandLine -Evidence $outcome.Evidence;continue};try{$json=$outcome.Stdout|ConvertFrom-Json;$resolved=[string]$json.services.sub2api.image;if($resolved -eq $mode.Image){Add-Result -Name ('Compose config '+$file.Name+' ['+$mode.Name+']') -Status PASS -Summary ('image 解析为 '+$mode.Image) -Command $outcome.CommandLine -Evidence $outcome.Evidence}else{Add-Result -Name ('Compose config '+$file.Name+' ['+$mode.Name+']') -Status FAIL -Summary ('解析结果为 '+$resolved+'，期望 '+$mode.Image) -Command $outcome.CommandLine -Evidence $outcome.Evidence}}catch{Add-Result -Name ('Compose config '+$file.Name+' ['+$mode.Name+']') -Status FAIL -Summary 'config 输出不是合法 JSON' -Command $outcome.CommandLine -Evidence $outcome.Evidence}}}
}

function Test-DockerDaemon {
    if($null -eq $DockerPath){Add-Result -Name 'Docker daemon 可用性' -Status BLOCKED -Summary '未找到 Docker CLI';return $false}
    $outcome=Invoke-External -FilePath $DockerPath -Arguments @('info','--format','{{.ServerVersion}}') -WorkingDirectory $RepoRoot -Timeout 20 -Name 'docker-daemon-info'
    if($outcome.ExitCode -eq 0 -and -not[string]::IsNullOrWhiteSpace($outcome.Stdout)){Add-Result -Name 'Docker daemon 可用性' -Status PASS -Summary ('daemon 可用，ServerVersion='+$outcome.Stdout.Trim()) -Command $outcome.CommandLine -Evidence $outcome.Evidence;$script:DockerDaemonAvailable=$true;return $true}
    Add-Result -Name 'Docker daemon 可用性' -Status BLOCKED -Summary 'Docker CLI 存在但 daemon 不可连接；未假装执行容器验证' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $false
}

function New-RuntimeOverride {
    $modelPath=$ModelDirForCompose.Replace('\','/')
    $lines=@('services:','  sub2api:',('    container_name: '+$ProjectName+'-app'),'    ports:',('      - "127.0.0.1:'+$ServerPort+':8080"'),'    volumes:','      - runtime_app_data:/app/data','  postgres:',('    container_name: '+$ProjectName+'-postgres'),'    volumes:','      - runtime_postgres_data:/var/lib/postgresql/data','  redis:',('    container_name: '+$ProjectName+'-redis'),'    volumes:','      - runtime_redis_data:/data','  intent-classifier:',('    container_name: '+$ProjectName+'-classifier'),'    ports:',('      - "127.0.0.1:'+$ClassifierPort+':8080"'),'    volumes:','      - type: bind',('        source: "'+$modelPath+'"'),'        target: /models','        read_only: true','      - runtime_classifier_state:/state','volumes:','  runtime_app_data:','  runtime_postgres_data:','  runtime_redis_data:','  runtime_classifier_state:')
    $path=Join-Path $RunRoot 'runtime.override.yml';[IO.File]::WriteAllLines($path,$lines,[Text.UTF8Encoding]::new($false));$script:RuntimeOverrideFile=$path
}
function Get-ComposeBaseArgs { param([Parameter(Mandatory)][string]$ComposeFile);return @('compose','--env-file',$ComposeEnvFile,'-f',$ComposeFile,'-f',$RuntimeOverrideFile,'-p',$ProjectName) }
function Run-ComposeOverrideConfigCheck {
    $base=Get-ComposeBaseArgs -ComposeFile $RuntimeComposeFile;$env=@{SUB2API_IMAGE=if($BuildDockerImages){$ProjectName+'-sub2api:runtime'}else{'ghcr.io/general-brash/personal_sub2:latest'};INTENT_CLASSIFIER_IMAGE=if($BuildDockerImages){$ProjectName+'-intent-classifier:runtime'}else{'ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.183-P1'}};$outcome=Invoke-External -FilePath $DockerPath -Arguments($base+@('config','--format','json')) -WorkingDirectory $RepoRoot -EnvironmentOverrides $env -Timeout 90 -Name 'compose-runtime-override-config';if($outcome.ExitCode -ne 0 -or $outcome.TimedOut){Add-Result -Name '隔离 Compose override config' -Status FAIL -Summary '临时卷/loopback 端口 override 解析失败' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $false};try{$json=$outcome.Stdout|ConvertFrom-Json;$sources=@();foreach($service in @('sub2api','postgres','redis')){foreach($volume in @($json.services.$service.volumes)){if($null -ne $volume.source){$sources+=[string]$volume.source}}};$repoDeploy=(Resolve-Path $DeployRoot).Path.Replace('\','/').TrimEnd('/');$unsafe=@($sources|Where-Object{$_.Replace('\','/').StartsWith($repoDeploy,[StringComparison]::OrdinalIgnoreCase)});if($unsafe.Count -gt 0){Add-Result -Name '隔离 Compose override config' -Status FAIL -Summary ('仍发现指向仓库 deploy 的 volume: '+($unsafe -join ', ')) -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $false};Add-Result -Name '隔离 Compose override config' -Status PASS -Summary '应用/PostgreSQL/Redis 使用临时卷，服务端口仅绑定 127.0.0.1' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $true}catch{Add-Result -Name '隔离 Compose override config' -Status FAIL -Summary '临时 override config 输出不是合法 JSON' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $false}
}
function Wait-ComposeHealth {
    param([Parameter(Mandatory)][string[]]$Services,[int]$WaitSeconds=180)
    $base=Get-ComposeBaseArgs -ComposeFile $RuntimeComposeFile;$deadline=(Get-Date).AddSeconds($WaitSeconds)
    do {
        $allHealthy=$true;$states=@()
        foreach($service in $Services){
            $outcome=Invoke-External -FilePath $DockerPath -Arguments($base+@('ps','-q',$service)) -WorkingDirectory $RepoRoot -Timeout 20 -Name ('compose-ps-'+$service);$containerId=$outcome.Stdout.Trim()
            if($outcome.ExitCode -ne 0 -or[string]::IsNullOrWhiteSpace($containerId)){$allHealthy=$false;$states+=($service+'=missing');continue}
            $inspect=Invoke-External -FilePath $DockerPath -Arguments @('inspect','--format','{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}',$containerId) -WorkingDirectory $RepoRoot -Timeout 20 -Name ('docker-health-'+$service);$state=$inspect.Stdout.Trim();$states+=($service+'='+$state);if($state -ne 'healthy'){$allHealthy=$false}
        }
        if($allHealthy){Add-Result -Name '隔离 Compose 服务健康状态' -Status PASS -Summary($states -join ', ');return $true}
        if((Get-Date)-lt$deadline){[System.Threading.Thread]::Sleep(3000)}
    }while((Get-Date)-lt$deadline)
    Add-Result -Name '隔离 Compose 服务健康状态' -Status FAIL -Summary '健康检查超时；证据见 compose ps/logs';$null=Invoke-External -FilePath $DockerPath -Arguments($base+@('logs','--no-color','--tail','200')) -WorkingDirectory $RepoRoot -Timeout 60 -Name 'compose-runtime-logs';return $false
}

$multiplierSql = @(
'CREATE TEMP TABLE rv_multiplier_results (table_name text NOT NULL, column_name text NOT NULL, value_text text NOT NULL, result text NOT NULL);',
'CREATE TEMP TABLE rv_model_probe (LIKE channel_model_pricing INCLUDING CONSTRAINTS);',
'INSERT INTO rv_model_probe SELECT * FROM channel_model_pricing LIMIT 1;',
'CREATE TEMP TABLE rv_interval_probe (LIKE channel_pricing_intervals INCLUDING CONSTRAINTS);',
'INSERT INTO rv_interval_probe SELECT * FROM channel_pricing_intervals LIMIT 1;',
'DO $rv$'
'DECLARE',
'    spec record;',
'    value_text text;',
'    row_count integer;',
'    rejected boolean;',
'BEGIN',
'    FOR spec IN SELECT * FROM (VALUES',
'        (''channel_model_pricing'', ''rv_model_probe'', ''fast_multiplier''),',
'        (''channel_model_pricing'', ''rv_model_probe'', ''flex_multiplier''),',
'        (''channel_pricing_intervals'', ''rv_interval_probe'', ''input_multiplier''),',
'        (''channel_pricing_intervals'', ''rv_interval_probe'', ''output_multiplier''),',
'        (''channel_pricing_intervals'', ''rv_interval_probe'', ''cache_write_multiplier''),',
'        (''channel_pricing_intervals'', ''rv_interval_probe'', ''cache_read_multiplier'')',
'    ) AS s(source_table, probe_table, column_name)',
'    LOOP',
'        EXECUTE format(''SELECT count(*) FROM %I'', spec.probe_table) INTO row_count;',
'        IF row_count = 0 THEN',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''NaN'', ''BLOCKED_EMPTY_TABLE'');',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''Infinity'', ''BLOCKED_EMPTY_TABLE'');',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''-Infinity'', ''BLOCKED_EMPTY_TABLE'');',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''0'', ''BLOCKED_EMPTY_TABLE'');',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''-1'', ''BLOCKED_EMPTY_TABLE'');',
'            CONTINUE;',
'        END IF;',
'        BEGIN',
'            EXECUTE format(''UPDATE %I SET %I = 1.25'', spec.probe_table, spec.column_name);',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''1.25'', ''PASS_POSITIVE'');',
'        EXCEPTION WHEN OTHERS THEN',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, ''1.25'', ''FAIL_POSITIVE:'' || SQLERRM);',
'        END;',
'        FOREACH value_text IN ARRAY ARRAY[''NaN'', ''Infinity'', ''-Infinity'', ''0'', ''-1'']',
'        LOOP',
'            rejected := false;',
'            BEGIN',
'                EXECUTE format(''UPDATE %I SET %I = %L::numeric'', spec.probe_table, spec.column_name, value_text);',
'            EXCEPTION WHEN OTHERS THEN',
'                rejected := true;',
'            END;',
'            INSERT INTO rv_multiplier_results VALUES (spec.source_table, spec.column_name, value_text, CASE WHEN rejected THEN ''REJECTED_INVALID'' ELSE ''ACCEPTED_INVALID'' END);',
'        END LOOP;',
'    END LOOP;',
'END',
'$rv$;'
'SELECT table_name || ''|'' || column_name || ''|'' || value_text || ''|'' || result FROM rv_multiplier_results ORDER BY table_name, column_name, value_text;'
) -join [Environment]::NewLine

function Run-ComposeDatabaseChecks {
    if(-not $DockerDaemonAvailable){Add-Result -Name 'PostgreSQL/Redis migration runtime' -Status BLOCKED -Summary 'Docker daemon 不可用，未启动数据库容器';Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status BLOCKED -Summary 'Docker daemon 不可用，未执行真实 PostgreSQL 约束探针';return}
    $base=Get-ComposeBaseArgs -ComposeFile $RuntimeComposeFile
    $migrationSql="SELECT COUNT(*) FROM schema_migrations WHERE filename IN ('226_add_usage_log_effective_model_indexes_notx.sql','227_composite_routes_add_cn_providers.sql','228_channel_pricing_multipliers.sql','229_plugins.sql','230_plugin_artifacts.sql','231_channel_pricing_multipliers_reject_nan.sql');"
    $migration=Invoke-External -FilePath $DockerPath -Arguments($base+@('exec','-T','postgres','psql','--no-psqlrc','-v','ON_ERROR_STOP=1','-qAt','-U','rv_sub2api','-d','rv_sub2api','-c',$migrationSql)) -WorkingDirectory $RepoRoot -Timeout 60 -Name 'postgres-migration-state'
    if($migration.ExitCode -eq 0 -and $migration.Stdout.Trim() -eq '6'){Add-Result -Name 'PostgreSQL/Redis migration runtime' -Status PASS -Summary 'schema_migrations 已记录官方 226-231 六个增量迁移' -Command $migration.CommandLine -Evidence $migration.Evidence}else{Add-Result -Name 'PostgreSQL/Redis migration runtime' -Status FAIL -Summary ('迁移记录检查失败，实际计数='+$migration.Stdout.Trim()) -Command $migration.CommandLine -Evidence $migration.Evidence}
    $redis=Invoke-External -FilePath $DockerPath -Arguments($base+@('exec','-T','redis','redis-cli','--no-auth-warning','ping')) -WorkingDirectory $RepoRoot -Timeout 30 -Name 'redis-ping';if($redis.ExitCode -eq 0 -and $redis.Stdout.Trim() -eq 'PONG'){Add-Result -Name 'Redis runtime connectivity' -Status PASS -Summary 'redis-cli ping 返回 PONG' -Command $redis.CommandLine -Evidence $redis.Evidence}else{Add-Result -Name 'Redis runtime connectivity' -Status FAIL -Summary 'Redis ping 失败' -Command $redis.CommandLine -Evidence $redis.Evidence}
    $multiplier=Invoke-External -FilePath $DockerPath -Arguments($base+@('exec','-T','postgres','psql','--no-psqlrc','-v','ON_ERROR_STOP=1','-qAt','-U','rv_sub2api','-d','rv_sub2api','-c',$multiplierSql)) -WorkingDirectory $RepoRoot -Timeout 90 -Name 'postgres-multiplier-runtime-probe'
    if($multiplier.ExitCode -ne 0 -or $multiplier.TimedOut){Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status FAIL -Summary 'PostgreSQL multiplier 探针执行失败' -Command $multiplier.CommandLine -Evidence $multiplier.Evidence;return}
    $lines=@($multiplier.Stdout -split '\r?\n'|Where-Object{$_.Trim()});$invalidAccepted=@($lines|Where-Object{$_ -match '\|ACCEPTED_INVALID$'});$positiveFailed=@($lines|Where-Object{$_ -match '\|FAIL_POSITIVE:'});$empty=@($lines|Where-Object{$_ -match '\|BLOCKED_EMPTY_TABLE$'});if($invalidAccepted.Count -gt 0 -or $positiveFailed.Count -gt 0){Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status FAIL -Summary ('发现未拒绝非法倍率 '+$invalidAccepted.Count+' 条，或正数写入失败 '+$positiveFailed.Count+' 条') -Command $multiplier.CommandLine -Evidence $multiplier.Evidence}elseif($empty.Count -gt 0){Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status BLOCKED -Summary ('真实表缺少可复制的基准行，无法执行写入探针；空表条目='+$empty.Count) -Command $multiplier.CommandLine -Evidence $multiplier.Evidence}else{Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status PASS -Summary '六个倍率列均接受正数并拒绝 NaN、Infinity、-Infinity、0、-1' -Command $multiplier.CommandLine -Evidence $multiplier.Evidence}
}

function Run-ComposeRuntime {
    if(-not $RunDockerRuntime){Add-Result -Name '隔离 Compose 最小服务流量' -Status NOT_RUN -Summary '默认不启动容器；传入 -RunDockerRuntime 才执行';return}
    if(-not $DockerDaemonAvailable){Add-Result -Name '隔离 Compose 最小服务流量' -Status BLOCKED -Summary 'Docker daemon 不可用；未启动、未拉取、未构建容器';return}
    if(-not(Run-ComposeOverrideConfigCheck)){return}
    $composeEnv=if($BuildDockerImages){@{SUB2API_IMAGE=$ProjectName+'-sub2api:runtime';INTENT_CLASSIFIER_IMAGE=$ProjectName+'-intent-classifier:runtime'}}else{@{SUB2API_IMAGE='ghcr.io/general-brash/personal_sub2:latest';INTENT_CLASSIFIER_IMAGE='ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.183-P1'}};$base=Get-ComposeBaseArgs -ComposeFile $RuntimeComposeFile
    if($BuildDockerImages){$build=Invoke-External -FilePath $DockerPath -Arguments($base+@('build','--pull','never')) -WorkingDirectory $RepoRoot -EnvironmentOverrides $composeEnv -Timeout 1800 -Name 'compose-build-local-runtime-images';if($build.ExitCode -ne 0 -or $build.TimedOut){Add-Result -Name '当前工作树 Docker 本地构建' -Status FAIL -Summary 'Compose build 失败；未执行 push/publish' -Command $build.CommandLine -Evidence $build.Evidence;return};Add-Result -Name '当前工作树 Docker 本地构建' -Status PASS -Summary '本地镜像构建完成；镜像未 push/publish' -Command $build.CommandLine -Evidence $build.Evidence}else{foreach($image in @($composeEnv.SUB2API_IMAGE,$composeEnv.INTENT_CLASSIFIER_IMAGE)){$inspect=Invoke-External -FilePath $DockerPath -Arguments @('image','inspect',$image) -WorkingDirectory $RepoRoot -Timeout 30 -Name ('image-inspect-'+(Get-Slug $image));if($inspect.ExitCode -ne 0 -or $inspect.TimedOut){Add-Result -Name '隔离 Compose 镜像存在性' -Status BLOCKED -Summary ($image+' 不在本地；为避免隐式 pull，未启动栈。使用 -BuildDockerImages 可从当前源码构建') -Command $inspect.CommandLine -Evidence $inspect.Evidence;return}};Add-Result -Name '隔离 Compose 镜像存在性' -Status PASS -Summary '两个指定镜像均已在本地；未执行隐式 pull'}
    try{$up=Invoke-External -FilePath $DockerPath -Arguments($base+@('up','-d','--no-build','--pull','never')) -WorkingDirectory $RepoRoot -EnvironmentOverrides $composeEnv -Timeout 600 -Name 'compose-runtime-up';if($up.ExitCode -ne 0 -or $up.TimedOut){Add-Result -Name '隔离 Compose 启动' -Status FAIL -Summary 'docker compose up 失败' -Command $up.CommandLine -Evidence $up.Evidence;return};Add-Result -Name '隔离 Compose 启动' -Status PASS -Summary '临时项目已启动' -Command $up.CommandLine -Evidence $up.Evidence;if(-not(Wait-ComposeHealth -Services @('postgres','redis','intent-classifier','sub2api') -WaitSeconds 180)){return};Run-ComposeDatabaseChecks;Run-ApplicationHttpChecks -BaseUrl ('http://127.0.0.1:'+$ServerPort);Run-ClassifierHttpChecks -BaseUrl ('http://127.0.0.1:'+$ClassifierPort);Add-Result -Name '支付/退款/HTTPS/内容审核/账号统计运行时流量' -Status NOT_RUN -Summary '最小流量已执行；完整支付事务、退款 provider、生产 HTTPS 和双策略审核需要专用 fixture，不能用健康检查冒充通过'}finally{$down=Invoke-External -FilePath $DockerPath -Arguments($base+@('down','--volumes','--remove-orphans')) -WorkingDirectory $RepoRoot -EnvironmentOverrides $composeEnv -Timeout 180 -Name 'compose-runtime-cleanup';if($down.ExitCode -ne 0 -or $down.TimedOut){Add-Result -Name '隔离 Compose 清理' -Status FAIL -Summary '临时栈清理失败，请根据证据手工确认项目资源' -Command $down.CommandLine -Evidence $down.Evidence}else{Add-Result -Name '隔离 Compose 清理' -Status PASS -Summary '已执行 down --volumes --remove-orphans' -Command $down.CommandLine -Evidence $down.Evidence}}
}
function Run-ExternalHttpInputs {if(-not[string]::IsNullOrWhiteSpace($AppBaseUrl)){Run-ApplicationHttpChecks -BaseUrl $AppBaseUrl};if(-not[string]::IsNullOrWhiteSpace($ClassifierBaseUrl)){Run-ClassifierHttpChecks -BaseUrl $ClassifierBaseUrl}}
function Capture-GitState {$git=Resolve-ToolPath 'git';if($null -eq $git){Add-Result -Name 'Git 状态保护快照' -Status BLOCKED -Summary '未找到 git';return $null};$outcome=Invoke-External -FilePath $git -Arguments @('-C',$RepoRoot,'status','--porcelain=v2','--branch','--untracked-files=all') -WorkingDirectory $RepoRoot -Timeout 30 -Name 'git-status-snapshot';if($outcome.ExitCode -ne 0 -or $outcome.TimedOut){Add-Result -Name 'Git 状态保护快照' -Status FAIL -Summary '无法读取 Git 状态' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $null};Add-Result -Name 'Git 状态保护快照' -Status PASS -Summary '已记录 branch/index/工作树状态；验证器不执行 reset/checkout/merge/commit/push' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return $outcome.Stdout}
function Compare-GitState {param([AllowNull()][string]$Before);if($null -eq $Before){return};$git=Resolve-ToolPath 'git';if($null -eq $git){return};$outcome=Invoke-External -FilePath $git -Arguments @('-C',$RepoRoot,'status','--porcelain=v2','--branch','--untracked-files=all') -WorkingDirectory $RepoRoot -Timeout 30 -Name 'git-status-after-validation';if($outcome.ExitCode -ne 0 -or $outcome.TimedOut){Add-Result -Name 'Git 现有状态未被验证器改变' -Status FAIL -Summary '无法读取验证结束后的 Git 状态' -Command $outcome.CommandLine -Evidence $outcome.Evidence;return};$beforeFiltered=(($Before -split '\r?\n')|Where-Object{$_ -notmatch '(?i)tools[/\\]runtime-validation[/\\]'}) -join [Environment]::NewLine;$afterFiltered=(($outcome.Stdout -split '\r?\n')|Where-Object{$_ -notmatch '(?i)tools[/\\]runtime-validation[/\\]'}) -join [Environment]::NewLine;if($beforeFiltered -eq $afterFiltered){Add-Result -Name 'Git 现有状态未被验证器改变' -Status PASS -Summary '除本运行器允许新增目录外，Git 状态前后一致' -Command $outcome.CommandLine -Evidence $outcome.Evidence}else{Add-Result -Name 'Git 现有状态未被验证器改变' -Status FAIL -Summary '验证器前后 Git 状态存在非允许差异' -Command $outcome.CommandLine -Evidence $outcome.Evidence}}

if(-not(Test-Path -LiteralPath (Join-Path $RepoRoot '.git') -PathType Container) -and -not(Test-Path -LiteralPath (Join-Path $RepoRoot '.git') -PathType Leaf)){throw('不是 Git 工作树: '+$RepoRoot)}
$gitBefore=Capture-GitState;$GoPath=Resolve-GoPath;$DockerPath=Resolve-ToolPath 'docker';$frontendDist=$null
if($null -eq $GoPath){Add-Result -Name 'Go 工具链' -Status BLOCKED -Summary '未找到 Go；无法执行后端测试和 embedded build'}else{$goVersion=Invoke-External -FilePath $GoPath -Arguments @('version') -WorkingDirectory $RepoRoot -Timeout 20 -Name 'go-version';if($goVersion.ExitCode -eq 0 -and -not $goVersion.TimedOut){Add-Result -Name 'Go 工具链' -Status PASS -Summary ($goVersion.Stdout.Trim()) -Command $goVersion.CommandLine -Evidence $goVersion.Evidence}else{Add-Result -Name 'Go 工具链' -Status FAIL -Summary 'Go version 命令失败' -Command $goVersion.CommandLine -Evidence $goVersion.Evidence}}
if($null -eq $DockerPath){Add-Result -Name 'Docker CLI' -Status BLOCKED -Summary '未找到 docker；Compose config 和容器检查无法执行'}else{$dockerVersion=Invoke-External -FilePath $DockerPath -Arguments @('version','--format','{{.Client.Version}}') -WorkingDirectory $RepoRoot -Timeout 20 -Name 'docker-client-version';Add-CommandResult -Outcome $dockerVersion -Name 'Docker CLI' -PassSummary ('Docker client='+$dockerVersion.Stdout.Trim()) -FailSummary 'Docker client version 失败'}
$envValues=New-ComposeEnvironment;$frontendDist=Run-FrontendChecks;Run-EmbeddedBackendBuild -FrontendDist $frontendDist;Run-ClassifierLocalTests;Run-ExistingShellContracts
Run-GoTest -Name '后端支付/退款/回跳单测' -Packages @('./internal/service','./internal/handler') -Regex 'Test(Refund|CanonicalizeReturnURL|BuildPaymentReturnURL|Payment|PaymentService|PublicOrderVerify)' -Tags unit -Timeout 600
Run-GoTest -Name '后端账号统计/视频/倍率单测' -Packages @('./internal/service') -Regex 'Test(CalculateStatsCost|ResolveAccountStatsCost|TryCustomRules|TryModelFilePricing|.*Video.*Billing.*|.*Multiplier.*)' -Tags unit -Timeout 600
Run-GoTest -Name '后端内容审核双策略与二审单测' -Packages @('./internal/service') -Regex 'Test(ContentModeration.*|.*SecondaryReview.*)' -Tags unit -Timeout 900
Run-GoTest -Name '后端 Plugin 单测' -Packages @('./internal/service','./internal/repository') -Regex 'Test(Plugin.*|.*Plugin.*)' -Tags unit -Timeout 600
Run-GoTest -Name '后端 migration/runner 合同单测' -Packages @('./migrations','./internal/repository') -Regex 'Test(.*Migration.*|.*Migrations.*|.*Multiplier.*)' -Tags unit -Timeout 600
Run-OptionalGoContract -Name '支付 pending/daily 并发专用测试' -SearchRoot $BackendRoot -NameRegex '(?i)(Pending|Daily).*(Concurrent|Concurrency)|(Concurrent|Concurrency).*(Pending|Daily)' -Package './internal/service' -Tags integration -Timeout 900
Run-OptionalGoContract -Name '负数退款拒绝专用测试' -SearchRoot $BackendRoot -NameRegex '(?i)(Negative|Invalid).*Refund|Refund.*(Negative|Invalid)' -Package './internal/service' -Tags unit -Timeout 600
Run-OptionalGoContract -Name 'HTTPS return URL 禁止降级专用测试' -SearchRoot $BackendRoot -NameRegex '(?i)(HTTPS|HTTP.*Downgrade|ReturnURL.*Scheme|Scheme.*ReturnURL)' -Package './internal/service' -Tags unit -Timeout 600
Run-OptionalGoContract -Name '账号统计与真实扣费差分专用测试' -SearchRoot $BackendRoot -NameRegex '(?i)(AccountStats.*Billing|Billing.*AccountStats|Stats.*(Match|Equal).*Billing|Billing.*Stats)' -Package './internal/service' -Tags integration -Timeout 900

Run-ComposeConfigChecks;$null=Test-DockerDaemon;Run-ExternalHttpInputs
if($RunDockerRuntime -or $BuildDockerImages){$RunDockerRuntime=$true}
if($RunDockerRuntime -or $BuildDockerImages){$RuntimeComposeFile=if($BuildDockerImages){Join-Path $DeployRoot 'docker-compose.dev.yml'}else{Join-Path $DeployRoot 'docker-compose.local.yml'};New-RuntimeOverride;Run-ComposeRuntime}
else{Add-Result -Name 'PostgreSQL/Redis migration runtime' -Status NOT_RUN -Summary '未传入 -RunDockerRuntime；运行器默认不启动容器';Add-Result -Name 'NaN/Infinity/非正数倍率数据库约束' -Status NOT_RUN -Summary '未传入 -RunDockerRuntime；运行器默认不启动容器'}
Compare-GitState -Before $gitBefore
$counts=@{};foreach($status in @('PASS','FAIL','BLOCKED','NOT_RUN')){$counts[$status]=@($Results|Where-Object{$_.Status -eq $status}).Count}
$summary=[ordered]@{schema_version='1';run_id=$RunId;generated_at=(Get-Date).ToString('o');repo_root=$RepoRoot;run_root=$RunRoot;docker_runtime_requested=[bool]$RunDockerRuntime;docker_build_requested=[bool]$BuildDockerImages;result_counts=$counts;results=$Results;safety=[ordered]@{repository_files_written=@('tools/runtime-validation/Invoke-RuntimeValidation.ps1','tools/runtime-validation/README.md');publish_push_tag_deploy_commands=$false;secrets_written_to_repo=$false;evidence_redacted=$true}}
$summaryPath=Join-Path $RunRoot 'summary.json';[IO.File]::WriteAllText($summaryPath,(($summary|ConvertTo-Json -Depth 10)+[Environment]::NewLine),[Text.UTF8Encoding]::new($false));Write-Host ('RUN_ROOT='+$RunRoot);Write-Host ('SUMMARY='+$summaryPath);Write-Host ('COUNTS PASS={0} FAIL={1} BLOCKED={2} NOT_RUN={3}' -f $counts.PASS,$counts.FAIL,$counts.BLOCKED,$counts.NOT_RUN);if($counts.FAIL -gt 0){exit 1};if($counts.BLOCKED -gt 0){exit 2};exit 0

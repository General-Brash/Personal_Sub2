# 178-P1 -> 179-183 运行时验证器

本目录只新增验证资产，不修改仓库已有源码、测试、Compose、workflow、.env、Git index 或工作树文件。

主运行器：

~~~powershell
C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1
~~~

## 安全边界

- 默认只读检查，不启动容器；BLOCKED 不会被伪装成通过。
- 所有日志、临时 env、前端构建产物、后端隔离副本、pytest 临时目录和汇总 JSON 写到 `%TEMP%\sub2api-runtime-validation\<run-id>`，不写入仓库。
- Compose env 中的密码、Bearer Token、签名密钥由运行器随机生成，并只传给子进程；输出会做值替换和 key/value 二次脱敏。
- 不调用 pnpm install，不清理 node_modules，不生成仓库内 frontend/dist 或 backend/internal/web/dist。
- 不执行 merge --continue、commit、push、tag、镜像 push/publish 或部署。
- Docker 运行模式使用独立项目名、临时卷、loopback 端口，并在 finally 中执行 down --volumes --remove-orphans。
- 当前工作树 Git 状态在运行前后比较；允许的新增路径仅为本目录验证文件。

## 默认检查内容

直接执行：

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1'
$rc = $LASTEXITCODE
$rc
~~~

默认会执行：

1. 记录 branch/index/worktree 状态，不改变 Git 状态。
2. 使用缓存的 Go 1.27 工具链（若存在）做后端相关单测、迁移/runner 合同单测和编译检查。
3. 直接复用现有 frontend/node_modules 做 vue-tsc、ESLint、根 Makefile 中全部 critical Vitest。
4. 使用 Vite --outDir 输出到 TEMP，再把临时前端产物复制到 TEMP 后端副本，执行 internal/web 编译和 cmd/server embedded build。
5. 用 python -m pytest -p no:cacheprovider 执行 Intent Classifier 现有 test_api.py；PYTHONPATH 和 pytest basetemp 均指向临时目录。
6. 在 bash 可用时复用现有 5 个部署合同脚本；bash 不可用记为 NOT_RUN。
7. 用临时 Compose env 对四个 Compose 文件分别执行 default/tag/digest 三种 docker compose config --format json；这一步不需要 daemon。
8. 检查 Docker CLI 和 daemon。daemon 不可用时记录 BLOCKED，不会假装执行 PostgreSQL/Redis/容器流量。
9. 对支付 pending/daily 并发、负数退款、HTTPS URL 降级、账号统计与真实扣费差分测试采用发现专用测试入口才执行的门禁；当前没有入口时明确记录 NOT_RUN。

## Docker/PostgreSQL/Redis 运行模式

### A. 从当前源码构建并运行（推荐）

需要 Docker Desktop Linux daemon 可用：

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' `
  -RunDockerRuntime `
  -BuildDockerImages
$rc = $LASTEXITCODE
$rc
~~~

说明：

- 使用 deploy/docker-compose.dev.yml 的当前工作树 build context。
- --pull never，避免验证器隐式联网拉取基础镜像；基础镜像不在本地时会真实失败。
- 主应用与 classifier 镜像仅保存在本地 Docker cache，不会 push。
- 应用、PostgreSQL、Redis 使用临时 Compose 卷；deploy/data、deploy/postgres_data、deploy/redis_data 不会被挂载。
- stack 启动后等待 postgres、redis、intent-classifier、sub2api healthy，然后执行数据库/Redis 探针和 loopback HTTP 探针。

### B. 使用本地已有镜像运行

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' -RunDockerRuntime
$rc = $LASTEXITCODE
$rc
~~~

该模式不会 pull；如果以下镜像不在本地，记为 BLOCKED：

~~~text
ghcr.io/general-brash/personal_sub2:latest
ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.183-P1
~~~

### C. 使用已运行的裸机服务做 HTTP 验证

不启动 Docker，传入 loopback 或明确的内网 URL：

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' `
  -AppBaseUrl 'http://127.0.0.1:8080' `
  -ClassifierBaseUrl 'http://127.0.0.1:18080' `
  -ClassifierModelDir 'C:\path\to\model-root' `
  -ClassifierModelVersion 'cyber-intent-v20260720.1'
$rc = $LASTEXITCODE
$rc
~~~

如果要验证 Plugin authenticated list request，可在当前 PowerShell 进程预先设置一个已授权的 admin Bearer Token，并只传变量名，不把 token 写在命令行：

~~~powershell
$env:SUB2API_RUNTIME_ADMIN_BEARER = '<仅在当前进程设置的临时值>'
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' `
  -AppBaseUrl 'http://127.0.0.1:8080' `
  -AppBearerTokenEnvName 'SUB2API_RUNTIME_ADMIN_BEARER'
~~~

运行器不会打印该值；验证结束后应由调用者清除当前进程环境变量。## 数据库探针覆盖

Docker stack 中应用启动会执行项目 migration runner，随后运行器通过 Compose 内部网络执行：

- schema_migrations 中官方 226、227、228、229、230、231 六个文件计数。
- Redis PING，期望 PONG。
- 在临时表中复制真实 channel_model_pricing 和 channel_pricing_intervals 的一行，逐列尝试合法 1.25 以及非法 NaN、Infinity、-Infinity、0、-1。
- 临时表没有基准行时记录 BLOCKED_EMPTY_TABLE，不把没有实际写入探针报告为通过。
- 如果数据库层接受任何非法倍率，记录 FAIL；如果全部拒绝并接受正数，记录 PASS。

已有 migration integration test 仍会由 Go testcontainers 体系单独执行，但只有 Docker daemon 可用时才会调用带 integration tag 的专用门禁。

## 支付、HTTPS、内容审核和账号统计

运行器会执行现有支付/退款/return URL、内容审核双策略、Plugin、账号统计/视频/倍率相关单测。对于本轮明确要求的新缺陷回归证明，运行器会先扫描当前工作树是否存在以下专用测试入口：

- pending/daily 并发限制；
- 负数退款拒绝；
- HTTPS 生产环境禁止 HTTP 降级；
- 账号统计与真实扣费的差分一致性。

找到入口才会执行，找不到就记录 NOT_RUN，后续补充测试后同一条命令即可重新验证。完整支付事务和 provider webhook 不会用无凭据健康请求冒充通过。

## Intent Classifier

默认本地 pytest test_api.py 已覆盖：

- /health/live 200；
- 无模型 /health/ready 503 合同；
- 有 fixture 模型时 /health/ready 200；
- 带 Bearer Token 的 /v1/classify 成功请求；
- 无 token、schema、请求体大小、并发和日志脱敏合同。

Docker/裸机 HTTP 模式只有在提供模型目录和版本时才会执行 /health/ready 与 /v1/classify。没有模型时只执行 /health/live，ready/classify 记录 NOT_RUN。

## 结果和证据

每次运行末尾打印：

~~~text
RUN_ROOT=<TEMP 下目录>
SUMMARY=<TEMP 下 summary.json>
COUNTS PASS=... FAIL=... BLOCKED=... NOT_RUN=...
~~~

结果状态语义：

- PASS：命令或 HTTP/数据库断言真实执行并满足预期。
- FAIL：命令真实执行但失败，或断言发现不符合预期。
- BLOCKED：前置环境不可用、镜像/模型/数据库缺失，无法诚实执行该项。
- NOT_RUN：本次按安全默认或缺少专用测试入口主动未执行。

退出码：

- 0：没有 FAIL，且没有 BLOCKED；
- 1：至少一个 FAIL；
- 2：没有 FAIL，但存在 BLOCKED。

summary.json 只保存脱敏结果和 TEMP 证据路径，不保存 Compose env 内容、token、密码或私钥。

## 当前环境不可用时的后续命令

### Docker daemon 可用后

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' -RunDockerRuntime -BuildDockerImages
~~~

### 只验证已有本地镜像

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' -RunDockerRuntime
~~~

### 使用实际模型进行 classifier ready/classify

~~~powershell
& 'C:\Users\exile\Documents\Codex\2026-05-17\integration-v0.1.183-p1\tools\runtime-validation\Invoke-RuntimeValidation.ps1' `
  -RunDockerRuntime `
  -ClassifierModelDir 'C:\path\to\model-root' `
  -ClassifierModelVersion 'cyber-intent-v20260720.1'
~~~

### 直接运行项目 Go integration tests

~~~powershell
$go = 'C:\Users\exile\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.0.windows-amd64\bin\go.exe'
& $go test -mod=readonly -tags=integration ./migrations ./internal/service `
  -run 'Test(ChannelPricingMultipliersNaNMigration_PostgresNormalizesAndRejectsNaN|PurchaseLimit|Refund)' `
  -count=1 -timeout=15m
~~~

该命令只在 Docker daemon/testcontainers 可用时才有意义；如果 provider 不健康，必须记录实际 skip/block 输出，不得将其表述为 runtime PASS。

### PostgreSQL/Redis 已独立可连接时

运行器当前优先采用隔离 Compose；如果不使用 Docker，建议先把实际服务映射到 loopback，再通过 AppBaseUrl/ClassifierBaseUrl 做 HTTP 验证。数据库迁移仍应使用一次性数据库或明确的临时 schema，禁止直接指向生产库。

## 与本轮禁止事项的关系

本目录验证器不负责也不会执行：

~~~text
merge --continue
commit
push
tag
镜像 push/publish
生产部署
~~~

这些动作必须由主理人另行决定并在运行时证据完整后单独授权。
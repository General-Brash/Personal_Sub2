# P4.2 应用修复、发布与生产升级

## 最终状态

- 2026-10-09 15:49:14（Asia/Shanghai）启动生产新容器，15:50:21 完成独立健康观察，15:51 完成主代理复核。
- 生产版本：`0.2.1-P4.2-hotfix.1`，替代 `0.2.1-P4.1.2-agents-a1-hotfix.1`。
- 业务源码提交：`c3b84b84ee3c9f151d9085376418d32f133a40c0`。
- 双架构镜像：`ghcr.io/general-brash/personal_sub2:0.2.1-P4.2-hotfix.1`。
- 不可变索引摘要：`sha256:a044c52f88b9a7fab0c47b1fab056c446f3d7a86b0f37da7b947cc260dd6aea9`。
- 生产 `SUB2API_IMAGE` 已固定为该摘要；没有覆盖旧 P4.2 标签，也没有更新 `latest` / `edge` 或创建新 Git Release。

## 根因修复

此前 P4.2 镜像从带 CRLF/混合换行的本地工作区构建，将非规范字节嵌入历史迁移；启动时与数据库 checksum 不一致。

此次未修改 SQL 逻辑、运行时迁移算法或数据库历史 checksum：

1. 增加 `backend/scripts/check-migration-line-endings.sh`，直接检查 SQL 原始字节；使用 `od`，避免 Git for Windows 的文本模式 grep 隐式剥离 CR。
2. 在三个源码编译 Dockerfile、两份 GoReleaser 配置、两份 Makefile 的构建入口接入检查。
3. CI 接入全量 SQL 检查和构建门禁回归测试，覆盖 LF 放行、CRLF/混合换行拒绝、空/缺失目录拒绝、检查不修改 SQL。
4. 以固定提交的 `git archive` 作为构建上下文，两个架构均在编译前通过 LF 检查。
5. 本地 242–244 工作区字节已恢复为 Git 中的 LF，Git SQL 内容无差异。
6. 新版本后缀用于区分修复产物，便于追踪及回退。

## 构建与验证

- 构建环境：本机 Docker Desktop / buildx，`linux/amd64`、`linux/arm64`，Go 1.27.2。
- 使用前次构建已验证的 Daocloud 基础镜像及 Alpine/Corepack/npm 镜像源；临时 Dockerfile 仅作镜像源替换，不改变应用源码。
- 两个架构实际运行 `sub2api -version`，版本与提交正确。
- 镜像已推送 GHCR，生产通过不可变摘要拉取并核验标签。
- 全量 migration 包单元测试通过。
- 迁移 runner checksum 兼容与拒绝未知 checksum 的定向测试通过。
- 构建门禁回归、脚本语法检查和现有 Docker runtime resources 回归通过。

## 隔离验证及生产切换

1. 重新备份当时完整数据库、应用数据目录、`.env` 与 Compose。
2. 完整还原到独立 PostgreSQL 18，搭配独立 Redis；网络设为 internal，不挂载生产数据卷，不允许访问公网供应商。
3. 使用实际发布镜像启动完整应用，而非仅运行迁移 helper；应用健康，310 条迁移记录以及权限、授权、价格和核心对象数量与还原前态一致。
4. 新定价管理 API 的匿名请求返回 401，没有变为公开接口；未执行真实改价或自动授予权限。
5. 使用 `nohup` 独立运行切换脚本：先通过环境变量指定新摘要，Compose 等待健康并观察至少 60 秒，成功后才原子持久化 `.env`；失败会自动换回旧镜像，不回滚数据库。
6. 原配置仅 `SUB2API_IMAGE` 改变，Compose 文件及其他环境配置逐字节未变。
7. 隔离容器、独立数据卷、网络和临时凭据已清理；旧生产镜像、备份和日志保留。

验证工具的两处问题均在生产切换前解决：隔离 PostgreSQL 初始化临时进程的就绪竞争，及 Python 默认 HTTP 探测返回 403。调整为 TCP 就绪检查、与既有健康检查一致的直连 curl 后通过；这些预检失败没有切换或中断生产应用。

## 生产复核

- 新版本/提交与目标镜像相符，容器 healthy，RestartCount=0。
- 本地、公网 `/health` 返回 `{"status":"ok"}`；公网首页 HTTP 200。
- 310 条 `schema_migrations` 完整记录未变，仍最新 247。
- 权限目录、授权、人工默认价与应用升级前快照完全一致。
- 用户 25、账号 214、API Key 75，与本次应用升级前快照一致。API Key 数量与更早数据库升级记录的 74 不同，75 已存在于本次切换前，不能归因为升级新增。
- 启动日志未发现 checksum mismatch、初始化失败或默认价刷新失败。

## 备份和证据

服务器目录（0700）：

`/home/taffy/session/backups/sub2api/20261009-P4.2-app-hotfix1/`

- `database.before.dump`，已通过完整隔离还原。
- SHA-256：`07768476e618ebeec3ff07704aafacca3707098458feb667c8a27883b12d374a`。
- `app-data.before.tar.gz`、旧配置、生产/隔离前后快照、实际操作脚本、启动和升级日志。
- `deployment-result.json`、`FINAL_VERIFIED`、`CLEANUP_COMPLETE`。

本地非敏感构建/发布证据：

`D:\Codex_Program\Personal_Sub2\release-artifacts\0.2.1-P4.2-hotfix.1\`

没有将生产业务备份或凭据下载到本地。若需要回退，应先回到保留的旧镜像；不能无条件恢复整库覆盖正常业务写入。

## 验证边界

未进行真实供应商多轮请求、管理员价格保存、全量业务回归或账单重算。隔离网络中的外部下载失败属于预期限制，隔离实例曾出现短暂后台查询超时；生产复核未发现默认价刷新失败。主分支推送触发的完整 CI 状态须以 GitHub 后续结果为准，不将本地定向验证冒充完整 CI 通过。

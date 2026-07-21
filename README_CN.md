# Personal_Sub2

Personal_Sub2 是基于 `1.6.0` 代码基线开发并独立维护的个人版本。

[English](README.md) | 中文 | [日本語](README_JA.md)

## 个人版内容

- **临时额度**：在永久余额之外，记录具有有效期的临时额度发放、消费和可用额度。
- **每日签到**：按可配置规则向用户发放临时额度奖励，并提供签到状态和记录。
- **银行**：支持预支临时额度、将永久额度兑换为临时额度，并提供可配置的限额、结算规则和流水记录。
- **安全审计二次审核**：改进 ASCII 关键词边界匹配；命中后可交给独立的 `intent-classifier` 服务二次判定，支持 `off`、`shadow`、`enforce` 模式，以及模型包校验、激活和回滚。

仓库不包含正式模型权重。启用模型二次判定前，请按 [`MODEL_PACKAGE.md`](services/intent-classifier/MODEL_PACKAGE.md) 准备并激活模型包。

## 安装与升级

脚本安装适用于已运行 PostgreSQL 和 Redis 的 Linux amd64/arm64 服务器，并需要 root 权限：

```bash
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash
```

安装后可访问 `http://服务器地址:8080` 完成首次设置。常用命令：

```bash
# 查看状态和日志
sudo systemctl status sub2api
sudo journalctl -u sub2api -f

# 升级到个人仓库的最新 Release
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash -s -- upgrade
```

也可以在管理后台使用版本检测和升级功能。执行升级前请备份数据库、配置文件和数据目录。

个人版容器镜像发布到：

```text
ghcr.io/general-brash/personal_sub2
```

部署文件及运行参数见 [`deploy/`](deploy/)；使用容器部署时，请确认应用镜像明确指向上述个人版镜像，避免混用其他版本。

## 从源码构建

环境要求：Go 1.26.5、Node.js 20+、pnpm 9、PostgreSQL 和 Redis。

```bash
git clone https://github.com/General-Brash/Personal_Sub2.git
cd Personal_Sub2

cd frontend
pnpm install --frozen-lockfile
pnpm run build

cd ../backend
go build -tags embed -ldflags="-X main.Version=$(./scripts/resolve-version.sh)" -o sub2api ./cmd/server
./sub2api
```

首次启动后访问 `http://localhost:8080`，按设置向导配置数据库、Redis 和管理员账号。

## 开发与验证

```bash
# 后端测试
cd backend
make test-unit

# 前端检查
cd ../frontend
pnpm run lint:check
pnpm run typecheck
pnpm run test:run
```

更多仓库内开发约定见 [`DEV_GUIDE.md`](DEV_GUIDE.md)。

## 安全与使用责任

- 使用前请确认符合所在国家或地区的法律法规，以及所接入服务的条款。
- 生产环境应使用独立强密码和固定密钥，并限制管理端和数据库的网络暴露范围。
- 不要提交或公开 API Key、访问令牌、数据库密码、`.env` 和 `config.yaml` 中的敏感信息。
- 升级、迁移或调整安全策略前，请先备份并在非生产环境验证。
- 本项目按现状提供；使用者自行承担账号、服务、数据和合规风险。

## 许可证

本项目按 [GNU Lesser General Public License v3.0](LICENSE)（或更高版本）授权。

# Sub2API Intent Classifier

独立的本地 ONNX 推理服务，仅在关键词命中后为 Sub2API 提供二次意图审核。

## HTTP 契约

- `GET /health/live`: 进程存活时返回 `200 {"status":"live"}`。
- `GET /health/ready`: 有有效 active 模型时返回 200；否则返回 503 和 `model_not_ready`。
- `POST /v1/classify`: V1 分类接口。成功响应只包含 Go 适配器允许的五个字段。
- `/admin/v1/models/**`: 仅 loopback 可访问，并要求独立管理员 Bearer Token。

## 环境变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `INTENT_CLASSIFIER_HOST` | `0.0.0.0` | HTTP 监听地址；systemd 建议设为 `127.0.0.1` |
| `INTENT_CLASSIFIER_PORT` | `8080` | HTTP 端口；systemd 建议设为 `18080` |
| `INTENT_CLASSIFIER_MODEL_ROOT` | `/models` | 版本化模型根目录 |
| `INTENT_CLASSIFIER_STATE_DIR` | `/state` | active/previous 指针的可写持久目录 |
| `INTENT_CLASSIFIER_ACTIVE_VERSION` | 空 | 显式启动版本，优先于持久状态 |
| `INTENT_CLASSIFIER_MODEL_VERSION` | 空 | 兼容旧名称，仅在 ACTIVE_VERSION 为空时读取 |
| `INTENT_CLASSIFIER_API_TOKEN` | 空 | 分类接口可选 Bearer Token |
| `INTENT_CLASSIFIER_ADMIN_TOKEN` | 空 | 管理接口必需的独立 Bearer Token |
| `INTENT_CLASSIFIER_ADMIN_URL` | 当前端口的 `127.0.0.1` URL | CLI 管理地址 |
| `INTENT_CLASSIFIER_MAX_CONCURRENCY` | `4` | 同时执行的推理上限，范围 1-64 |
| `INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS` | `250` | HTTP有界等待时间，范围1-30000毫秒 |
| `INTENT_CLASSIFIER_MAX_REQUEST_BYTES` | `65536` | HTTP 请求体上限 |
| `INTENT_CLASSIFIER_LOG_LEVEL` | `INFO` | JSON 日志级别 |

服务日志不会记录请求正文、命中关键词或鉴权头。

## 本地运行

```bash
python -m venv .venv
.venv/bin/pip install -e .
INTENT_CLASSIFIER_MODEL_ROOT=/models \
INTENT_CLASSIFIER_STATE_DIR=/state \
INTENT_CLASSIFIER_ACTIVE_VERSION=cyber-intent-v1 \
.venv/bin/intent-classifier serve
```

Windows PowerShell 可使用 `.venv\Scripts\intent-classifier.exe`。

## 模型管理

```bash
intent-classifier install /srv/sub2api-model-import/cyber-intent-v1 --models-dir /models
intent-classifier validate cyber-intent-v1 --model-root /models
intent-classifier preload cyber-intent-v1
intent-classifier activate cyber-intent-v1
intent-classifier list
intent-classifier rollback
```

`install` 只接受已经导出的模型包目录，不接受 zip。它从严格校验后的 manifest 读取版本号，在模型根目录内完成复制、权限归一、二次校验和原子发布；目标版本已存在时拒绝覆盖。来源文件的执行位不参与安全判断，以兼容 Windows Docker bind 等跨文件系统挂载；文件系统支持 `chmod` 时，发布后的目录统一为 `0755`、普通文件统一为 `0644`。明确不支持权限变更的挂载会跳过 mode 归一，但其他 I/O 错误仍会失败。仅执行这个一次性命令时将模型目录挂载为可写，常驻服务仍应只读挂载。

`preload`、`activate`、`list`、`rollback` 从 `INTENT_CLASSIFIER_ADMIN_TOKEN` 读取管理令牌，并通过 loopback 管理 API 操作正在运行的进程。退出码：成功 `0`，参数、离线包校验或安装失败 `2`，管理请求被拒绝或冲突 `3`，服务或网络不可用 `4`。

模型包的冻结规范见 [MODEL_PACKAGE.md](MODEL_PACKAGE.md)。

## Docker

构建上下文必须是本目录：

```bash
docker build -t sub2api-intent-classifier:latest .
docker run --rm \
  -p 127.0.0.1:18080:8080 \
  -v /srv/sub2api/models:/models:ro \
  -v /srv/sub2api/intent-state:/state \
  -e INTENT_CLASSIFIER_ADMIN_TOKEN=replace-me \
  sub2api-intent-classifier:latest
```

镜像健康检查使用 Python 标准库访问 `/health/live`，不依赖 curl 或 wget。模型目录可且应只读挂载。

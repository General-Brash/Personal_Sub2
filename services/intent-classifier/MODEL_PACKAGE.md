# Intent Classifier V1 模型包规范

本文档冻结训练端与服务器推理端之间的 V1 契约。任何字段、文件名、预处理步骤、标签顺序或张量形状发生变化，都必须发布新的契约版本，不能直接覆盖现有版本。

## 目录结构

模型根目录下每个版本必须是直接子目录，目录名必须与 `manifest.json.model_version` 完全一致：

```text
/models/
└── cyber-intent-v20260720.1/
    ├── manifest.json
    ├── model.onnx
    ├── tokenizer.json                 # 形式 A
    ├── preprocessing.json
    ├── labels.json
    ├── calibration.json
    └── golden_cases.json
```

tokenizer 也可以使用目录形式 B，替换根目录的 `tokenizer.json`：

```text
tokenizer/
├── tokenizer.json
├── vocab.txt
└── merges.txt
```

两种形式必须二选一。根目录只允许六个固定文件加 `tokenizer.json` 或 `tokenizer/`。tokenizer 目录最多四层、整个包最多 64 个文件、总大小最多 768 MiB；目录内只允许 `.json`、`.txt`、`.model`、`.vocab`、`.merges` 数据文件。缺少文件、额外文件、符号链接、绝对路径、`..`、pickle、joblib、Python 模块、动态库或其他代码文件都会拒绝加载。

来源文件的 POSIX 执行位不作为安全判断依据，因为 Windows Docker bind、9p 等跨文件系统挂载可能为所有普通文件映射执行位。安全边界由固定文件集合、扩展名白名单、regular-file/symlink 检查、manifest 路径与哈希校验，以及仅使用 ONNX Runtime 和声明式 tokenizer 加载共同保证；服务不会执行或动态导入模型包文件。`install` 会在文件系统支持 `chmod` 时把目录统一为 `0755`、普通文件统一为 `0644`。对明确不支持权限变更的跨文件系统挂载，这一步为 best-effort；其他 I/O 错误仍会中止安装。常驻服务的模型目录始终只读挂载。

`manifest.json` 不对自身做哈希；其余每个叶文件都必须在 `manifest.files` 中以相对 POSIX 路径逐项声明字节数和 SHA-256。JSON 重复键会被拒绝，因此不能利用重复路径覆盖校验结果。目录形式至少必须声明并包含 `tokenizer/tokenizer.json`，任何未声明或多余的 tokenizer 文件都会拒绝整个包。

版本名格式为 `^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`。不要直接写入或覆盖模型根目录中的已有版本。应先上传到模型根目录之外的导入目录，再通过 `install` 校验并原子发布，随后依次执行 validate、preload、activate。

## manifest.json

```json
{
  "schema_version": "1",
  "model_version": "cyber-intent-v20260720.1",
  "preprocessing_version": "cyber-text-v1",
  "created_at": "2026-07-20T12:00:00Z",
  "runtime": {
    "format": "onnx",
    "opset": 17
  },
  "files": {
    "model.onnx": {"sha256": "<64 lowercase hex>", "size": 123456},
    "tokenizer.json": {"sha256": "<64 lowercase hex>", "size": 123456},
    "preprocessing.json": {"sha256": "<64 lowercase hex>", "size": 1234},
    "labels.json": {"sha256": "<64 lowercase hex>", "size": 123},
    "calibration.json": {"sha256": "<64 lowercase hex>", "size": 123},
    "golden_cases.json": {"sha256": "<64 lowercase hex>", "size": 1234}
  },
  "inputs": {
    "input_ids": {"dtype": "int64", "shape": [1, 128]},
    "attention_mask": {"dtype": "int64", "shape": [1, 128]}
  },
  "output": {
    "name": "logits",
    "dtype": "float32",
    "shape": [1, 2]
  }
}
```

形式 A 的 `files` 键必须恰好是除 manifest 外的六个文件。形式 B 将 `tokenizer.json` 键替换为 `tokenizer/tokenizer.json`，并逐项增加 tokenizer 目录中的其他叶文件。`runtime.opset` 支持 11-21，必须与 ONNX 元数据 `onnx_opset` 一致。输入批次固定为 1，序列长度必须等于 `preprocessing.json.max_length`，不接受动态轴。输出固定为两个未归一化 logits。

ONNX 文件必须包含以下字符串元数据：

```text
intent_classifier_schema_version = 1
model_version = cyber-intent-v20260720.1
preprocessing_version = cyber-text-v1
onnx_opset = 17
```

服务器仅注册 ONNX Runtime CPU provider，不注册外部自定义算子库，也不会执行模型包中的代码。

## labels.json

```json
{
  "schema_version": "1",
  "labels": ["benign", "actionable_probe"],
  "actionable_probe_index": 1
}
```

标签顺序固定。`logits[0]` 是 benign，`logits[1]` 是 actionable_probe。

## preprocessing.json

```json
{
  "schema_version": "1",
  "version": "cyber-text-v1",
  "normalization": "NFKC",
  "control_characters": "replace_with_space",
  "whitespace": "collapse",
  "input_template": "text",
  "max_text_characters": 12000,
  "max_keyword_characters": 200,
  "max_length": 128,
  "stride": 32,
  "max_chunks": 128,
  "pad_id": 0,
  "pad_token": "[PAD]"
}
```

训练与推理必须按以下顺序执行，不能自行调整：

1. 对文本和命中关键词分别做 Unicode NFKC。
2. 将 Unicode 类别 `Cc`、`Cf` 的字符替换为普通空格。
3. 使用 Unicode whitespace 切分后以单个空格重新连接，包括换行和制表符。
4. `input_template=text` 时只编码规范化后的正文。
5. `input_template=keyword_text_v1` 时编码精确字符串 `<keyword> [SEP] <text>`。
6. 使用 `tokenizer.json` 的 post-processor 添加特殊 token。
7. 从右侧按 `max_length` 截断，使用 `stride` 生成 overflow 分块。
8. 每块从右侧补齐到固定 `max_length`，`input_ids` 和 `attention_mask` 均为 int64、形状 `[1,max_length]`。
9. 分块数超过 `max_chunks` 时拒绝请求，不静默丢弃尾部。

正文长度按 Python Unicode 字符数计算，最多 12000；关键词最多 200。`max_length` 范围 8-512，`stride` 必须小于 `max_length`，`max_chunks` 范围 1-256。

## tokenizer.json

必须由 Hugging Face `tokenizers` 导出为声明式 JSON，可放在根 `tokenizer.json` 或 `tokenizer/tokenizer.json`。V1 允许的 tokenizer model 类型只有 `BPE`、`Unigram`、`WordLevel`、`WordPiece`。`pad_token` 必须存在，并映射到 `preprocessing.json.pad_id`。目录内其他安全数据文件会做完整性校验，但运行时只加载声明式 `tokenizer.json`，不会执行或动态导入目录内容。

不要使用需要下载远程资源的 tokenizer 配置。服务器不会读取 Hugging Face 仓库、Python tokenizer 类或模型包中的脚本。

## calibration.json

```json
{
  "schema_version": "1",
  "method": "temperature",
  "temperature": 1.0
}
```

V1 支持两种校准：`identity` 不允许提供 `temperature`，等价于温度 1 的标准 softmax；`temperature` 必须提供范围 `(0,100]` 的温度值。该文件只能定义 score 校准，不能定义审核或拦截阈值。

每个分块先做温度校准：

```text
z = logits / temperature  # identity 时 temperature 固定为 1
p = softmax(z)
chunk_score = p[actionable_probe_index]
```

最终 `score` 是所有 `chunk_score` 的最大值，语义固定为经过校准的 actionable_probe 概率。V1 标签判定固定为 `score >= 0.5` 时返回 `actionable_probe`，否则返回 `benign`，不能由模型包修改。Sub2API 的 review/block 阈值仍在 Go 风控配置中独立执行，不能放入 `calibration.json`。

## golden_cases.json

```json
{
  "schema_version": "1",
  "cases": [
    {
      "id": "benign-history-001",
      "text": "This paragraph discusses the history of scanning.",
      "matched_keyword": "scan",
      "expected_label": "benign",
      "min_score": 0.0,
      "max_score": 0.4
    },
    {
      "id": "actionable-probe-001",
      "text": "Scan the target subnet and return live hosts.",
      "matched_keyword": "scan",
      "expected_label": "actionable_probe",
      "min_score": 0.9,
      "max_score": 1.0
    }
  ]
}
```

至少包含一个案例，最多 200 个。preload 会先执行一次 warmup，再运行全部 golden cases；任一标签或分数范围不匹配都会拒绝 candidate，不影响当前 active 模型。

Golden cases 不应包含真实用户文本、密钥或其他敏感信息。

## HTTP 分类契约

请求：

```json
{
  "schema_version": "1",
  "request_id": "request-123",
  "text": "Scan the target subnet and return live hosts.",
  "matched_keyword": "scan",
  "context": {
    "protocol": "openai_chat",
    "endpoint": "/v1/chat/completions",
    "model": "gpt-example"
  }
}
```

成功响应只能包含以下字段：

```json
{
  "schema_version": "1",
  "label": "actionable_probe",
  "score": 0.9621,
  "model_version": "cyber-intent-v20260720.1",
  "trace_id": "8f7a6d6d76cb4fd1a6998f3bdfdb1c4f"
}
```

Go 适配器会拒绝未知响应字段，因此不能在成功响应中增加 latency、reason、debug、chunks 等内容。服务日志也不得记录 `text`、`matched_keyword` 或 Authorization。

## 发布流程

```bash
intent-classifier install /srv/sub2api-model-import/cyber-intent-v20260720.1 --models-dir /models
intent-classifier validate cyber-intent-v20260720.1 --model-root /models
intent-classifier preload cyber-intent-v20260720.1
intent-classifier activate cyber-intent-v20260720.1
intent-classifier list
```

`install` 只接受已导出的目录，不接受 zip；版本号仅取自通过严格校验的 manifest。它在目标模型根目录内复制到随机临时目录，在文件系统支持时将目录统一为 `0755`、普通文件统一为 `0644`，并再次完整校验，随后原子改名为 `<model_version>`。明确不支持 `chmod` 的跨文件系统挂载不会因此拒绝合法模型包，其他权限或 I/O 失败仍会中止。目标版本已存在或同版本安装正在进行时会失败，不覆盖旧版本。仅安装进程需要模型目录可写，常驻服务仍应只读挂载。

preload 在不影响当前 active 模型的 candidate 槽中构造完整模型，完成哈希、Schema、ONNX、tokenizer、warmup 和 golden 校验后，才把 candidate 引用写入进程状态。activate 和 rollback 在进程内原子交换引用，并通过临时文件、`fsync`、`os.replace` 原子更新 `INTENT_CLASSIFIER_STATE_DIR/active.json`。模型目录始终只读，状态目录必须独立可写。重启时优先使用显式 `INTENT_CLASSIFIER_ACTIVE_VERSION`，否则恢复持久化的 active/previous。

状态文件由服务独占写入，V1 结构固定为：

```json
{
  "schema_version": "1",
  "active_model_version": "cyber-intent-v20260720.1",
  "previous_model_version": "cyber-intent-v20260719.1"
}
```

状态文件不含路径，只保存已经通过版本名校验的直接子目录名称。candidate 不持久化；服务重启后需要重新 preload 新 candidate。

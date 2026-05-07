# cycle 上层闭环 + 云文档阶段日志

`/eval-search cycle` 是 `/eval-search run`、`/eval-search report`、`/eval-search propose-pr` 的上层编排入口。用户只触发一次，主 agent 负责按阶段推进、记录状态、遇到失败时停止并给出可恢复位置。

## 入口

```text
/eval-search cycle [--subset N]
                   [--loader-profile <base-reader>]
                   [--executor-profile <blind-runner>]
                   [--report-doc <doc-url-or-token>]
                   [--create-report-doc]
                   [--report-parent-token <folder-or-wiki-node-token>]
                   [--skip-pr]
```

- `--report-doc`：把本轮阶段日志追加到已有云文档。
- `--create-report-doc`：未传 `--report-doc` 时创建新云文档；默认创建到当前用户个人空间，可选 `--report-parent-token`。
- `--skip-pr`：只跑到打分/report，不进入 optimizer 和 PR 创建。
- 未指定云文档参数时，默认创建新报告文档。除非用户明确禁止云文档记录，否则 cycle 不走纯本地日志模式。

## 状态文件

cycle 必须先创建本地状态，再调用任何飞书或 GitHub 写操作：

```text
tests/eval-search/runs/<run-id>/
├── cycle.json
└── cloud-doc/
    ├── 00-created.md
    ├── 10-run-started.md
    ├── 20-run-finished.md
    ├── 30-score-finished.md
    ├── 40-pr-finished.md
    └── tainted_tokens.json
```

`cycle.json` 结构：

```json
{
  "cycle_id": "2026-05-07T03-30Z",
  "run_id": "2026-05-07T03-30Z",
  "status": "running",
  "started_at": "2026-05-07T03:30:00Z",
  "ended_at": null,
  "cloud_doc": {
    "url": "",
    "token": "",
    "created_by_cycle": true,
    "tainted": true
  },
  "stages": [],
  "pr_urls": []
}
```

每次阶段状态变化都按顺序执行：

1. 更新 `cycle.json`
2. 渲染一个 `cloud-doc/<nn>-<stage>.md`
3. 追加到云文档
4. 只有云文档追加成功后才进入下一个阶段

若云文档追加失败，重试一次；仍失败则停止 cycle，把失败写入 `cycle.json`，不要继续提 PR。

## 完成定义（必须满足）

`/eval-search cycle` 不是本地脚本跑完就结束。未传 `--skip-pr` 时，必须同时交付：

- `summary.json` / `verdicts.json` 已写入本地 run 目录
- 云文档已创建或追加成功，且文档 URL 写入 `cycle.json.cloud_doc.url`
- 云文档 token 已写入 `cloud-doc/tainted_tokens.json`
- draft PR 已创建，PR URL 写入 `cycle.json.pr_urls`
- PR description 包含云文档 URL、run-id、分数摘要、污染摘要和未处理 finding
- PR URL 已追加回云文档的 final 段
- 最终回复用户时同时给出云文档 URL 和 PR URL

任一必需链接缺失时，cycle 状态只能是 `failed` 或 `blocked`，不能回复“已完成”。

## 云文档创建 / 追加

创建新文档：

```bash
lark-cli docs +create --api-version v2 --as user \
  --doc-format markdown \
  --content @tests/eval-search/runs/<run-id>/cloud-doc/00-created.md \
  --jq '.data.document.url'
```

创建到指定目录：

```bash
lark-cli docs +create --api-version v2 --as user \
  --parent-token '<folder-or-wiki-node-token>' \
  --doc-format markdown \
  --content @tests/eval-search/runs/<run-id>/cloud-doc/00-created.md \
  --jq '.data.document.url'
```

追加阶段日志：

```bash
lark-cli docs +update --api-version v2 --as user \
  --doc '<report-doc-url-or-token>' \
  --command append \
  --doc-format markdown \
  --content @tests/eval-search/runs/<run-id>/cloud-doc/20-run-finished.md
```

Markdown 文件必须使用 `@file` 传参，避免 shell 转义破坏表格、链接或代码块。

## 云文档内容边界

云文档是给人看进度和 review 结果的，不是评测原始数据仓库。允许写：

- cycle-id / run-id / git head / 分支 / 账号类型
- stage 状态、开始结束时间、失败原因
- dataset 数量、preflight 污染数量、executor 完成数量
- 总分、各维度均值、finding 聚类摘要、PR URL
- 本地产物路径，例如 `tests/eval-search/runs/<run-id>/summary.json`

禁止写：

- `dataset.jsonl` 全量内容
- 标准答案、source URLs、rubric 的 per-case 原文
- 完整 trajectory、完整 verdict rationale、key_error_snippets
- 任何 access token、app secret、cookie、GitHub token

per-case 信息只允许写 `case_id`、分数、桶归因和一句不含标准答案的摘要。

## 阶段编排

### 0. setup

- 确认 repo 路径和分支
- 确认 `lark-cli auth status`、`gh auth status`
- 生成 `run-id`
- 创建 `cycle.json`
- 创建或绑定云文档
- 把云文档 token 写入 `cloud-doc/tainted_tokens.json`

setup 文档段落必须包含醒目的污染声明：

```markdown
# eval-search cycle <run-id>

> This document is eval-search process material. It may contain benchmark summaries and must be treated as tainted for future search evaluations.

| Field | Value |
|---|---|
| Run ID | `<run-id>` |
| Status | `setup started` |
```

### 1. run

内部执行 `/eval-search run` 的流程：拉数据集、污染预检、Executor、Judge、聚合。

阶段日志至少追加两次：

- `run started`：记录 run-id、subset、loader/executor profile、run 目录
- `run finished`：记录 dataset size、scored count、skipped count、trajectory 数、summary 路径

### 2. score/report

读取 `summary.json` 和 `verdicts.json`，形成面向人的摘要。该阶段不重新打分，只消费 run 阶段已经产出的 Judge 结果。

必须记录：

- 总分 / 满分 / 百分比
- recall / accuracy / completeness / contamination_penalty 的总和与均值
- top findings，最多 10 条
- tainted fetch cases 数量和 case_id 列表

### 3. propose-pr

未传 `--skip-pr` 时进入该阶段。内部执行 `/eval-search propose-pr <run-id>`：

- Optimizer 生成 diff
- 主 agent 复查 PR 颗粒度和白名单
- 质量门禁
- regression 重跑
- 生成 PR description，并把云文档 URL 写入 description
- 创建 draft PR，记录返回的 PR URL
- 立刻把 PR URL 回写到 `cycle.json.pr_urls`
- 追加 `40-pr-finished.md` 到云文档，包含 PR URL

云文档记录：

- PR URL / state / draft 状态
- touched files
- quality gate 结果
- before/after 分数摘要
- 未处理归因

如果没有可提交改动，记录 `no-op`，不创建空 PR。

PR 创建失败时，必须把失败原因、当前分支、commit sha、可恢复命令写入云文档；不得只在本地终端输出错误。

### 4. final

更新 `cycle.json.status`：

- `completed`：所有启用阶段完成
- `completed_without_pr`：`--skip-pr` 或 no-op
- `failed`：任一必需阶段失败

最后追加一段总览，包含下一步建议和恢复命令：

```markdown
## Final

| Field | Value |
|---|---|
| Status | completed |
| Run ID | `<run-id>` |
| Summary | `tests/eval-search/runs/<run-id>/summary.json` |
| PR | `<url or none>` |
| Report Doc | `<cloud-doc-url>` |
```

最终回复必须包含：

```text
PR: <url or none>
Cloud report: <url>
Run ID: <run-id>
```

## 污染控制

cycle 生成或更新的云文档默认是 tainted/process material。规则：

1. 创建或绑定文档后，立刻提取 doc token，写入 `cloud-doc/tainted_tokens.json`
2. 本 cycle 的 regression / after-run 必须把该 token 作为额外 tainted token
3. 未来持久 blocklist 需要单独处理：
   - 单独开 `chore(eval-search): blocklist cycle report <run-id>` PR；或
   - 在云文档无法被 executor 账号搜索到的前提下，在本轮报告中说明未持久化 blocklist
4. 不得把 blocklist 更新混入 `search_strategy`、`skill_prompts` 或 `tool_capability` 优化 PR

## 恢复策略

- `setup` 失败：修复认证或文档权限后，重新执行 cycle
- `run` 失败：保留 `cycle.json`，从已有 `run-id` 的本地 artifact 判断是否能补跑缺失 case；不能补跑则新 cycle
- `score/report` 失败：不重跑 Executor，只重新读取 `summary.json` / `verdicts.json` 并追加云文档
- `propose-pr` 失败：修复 git/gh/quality gate 后，从同一 `run-id` 重新执行 propose-pr 阶段，并追加恢复记录

任何恢复都必须追加云文档段落，不得静默覆盖既有记录。

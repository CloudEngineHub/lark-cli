# run 目录结构 + 中间产物约定

## 目录位置

```
<repo-root>/tests/eval-search/runs/<run-id>/
```

`<run-id>` 格式：`YYYY-MM-DDTHH-MMZ`（UTC，用 `date -u +%Y-%m-%dT%H-%MZ` 生成）。

整个 `tests/eval-search/runs/` 被 gitignore，不进版本库。

确定性 setup runner：

```bash
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --loader-profile <base-reader> \
  --executor-profile <blind-runner> \
  --subset 3
```

runner 只负责创建 run 目录、拉取并转换 live dataset、检查 executor 账号隔离、写 `preflight.json`。它不会执行 AI Executor/Judge 阶段；setup 成功时 `summary.json.status` 为 `ready_for_executor`。

单账号时间隔离模式：

```bash
node --experimental-strip-types tests/eval-search/eval-search-run.ts --snapshot-only --loader-profile <base-reader>
# 移除该账号的评测 Base 权限
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --dataset-file tests/eval-search/runs/<snapshot-run>/dataset.jsonl \
  --executor-profile <blind-runner>
```

第一步只写本地 `dataset.jsonl`，`summary.json.status` 为 `snapshot_ready`。第二步会复制该 dataset 到新的 run 目录，并重新检查 executor 已经不能读取评测 Base。

## 单次 run 目录布局

```
tests/eval-search/runs/2026-04-15T10-00Z/
├── cycle.json              # 仅 /eval-search cycle 阶段编排使用；记录云文档、阶段状态、PR URL
├── cloud-doc/              # 仅 /eval-search cycle 使用；每次追加云文档前生成的 markdown 片段
│   ├── 00-created.md
│   ├── 20-run-finished.md
│   └── tainted_tokens.json
├── meta.json               # run 元信息（cli 版本、loader/executor profile、账号、开始/结束时间）
├── raw/
│   ├── base_records_pages.json
│   └── base_records_combined.json
├── dataset.jsonl           # 从 base 拉下来并转换的 cases
├── preflight.json          # 污染预检结果
├── trajectories/
│   ├── case_001.json       # Executor 增量写盘，崩溃可恢复
│   ├── case_002.json
│   └── ...
├── verdicts.json           # Judge 产出
├── summary.json            # 聚类后的 findings
└── pr-draft/               # 仅 propose-pr 阶段产出
    ├── diff.patch
    ├── generalization_note.json
    ├── unhandled_findings.md
    ├── commit_message.txt
    └── after_verdicts.json # regression 重跑结果（不含 trajectories，减小体积）
```

## meta.json

```json
{
  "run_id": "2026-04-15T10-00Z",
  "started_at": "2026-04-15T10:00:13Z",
  "ended_at": "2026-04-15T11:42:51Z",
  "lark_cli_version": "v1.0.11+git-abc1234",
  "git_head": "abc1234",
  "git_dirty": true,
  "loader_profile": "base-reader",
  "executor_profile": "eval-search",
  "user_open_id": "ou_xxx",
  "user_name": "eval-search-bot",
  "subset": null,
  "cases_scored": 13,
  "cases_skipped_contamination": 0,
  "cases_skipped_parse_error": 1
}
```

`git_dirty=true` 的 run 打上 `⚠️ dirty` 标记；propose-pr 阶段若源码 dirty 会拒绝生成 PR（否则 diff 混入无关改动）。

## 增量持久化约定

Executor 每完成 1 round（= 1 次 lark-cli 调用 + 解析），追加写入 `trajectories/<case_id>.json`：

```json
{
  "case_id": "case_001",
  "query": "...",
  "started_at": "...",
  "rounds": [
    {"idx": 1, "tool": "Read", "target": "skills/lark-doc/SKILL.md", "outcome_summary": "..."},
    {"idx": 2, "tool": "Bash", "cmd": "lark-cli drive +search --query '华东 Aily'", "outcome_summary": "top-3: ..."},
    ...
  ],
  "answer": null,
  "gave_up": false,
  "ended_at": null
}
```

所有未闭合的 case（`ended_at: null`）在 run 结束时标记为 `incomplete`，Judge 按 `gave_up=true` 处理但 `rounds_used` 如实记录。

## 并发度

v0.1 建议 **串行跑 Executor**：
- 避免多 sub-agent 同时打飞书 API 触发限流
- v2 历史上 sub-agent 529 频繁，并发会放大问题
- 评测 13 case 串行约 1-2 小时，可接受

未来若评测集扩到 50+ case，再考虑 semaphore 限并发 = 2。

## 清理策略

`tests/eval-search/runs/` 不自动清理。用户手动 `rm -rf tests/eval-search/runs/<run-id>` 或按时间删旧的。

.gitignore 已覆盖整个 runs/ 目录。

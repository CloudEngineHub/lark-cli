---
name: eval-search
version: 0.1.0
description: "lark-cli 搜索能力端到端评测 Harness：拉取飞书评测集 → 盲测执行 → 四维打分 → 聚合归因 → 自动生成 PR 草稿。当用户要评测 lark-cli 搜索效果、做 v_n→v_{n+1} 迭代、让新人跑一轮优化闭环时使用。"
metadata:
  requires:
    bins: ["node", "lark-cli", "jq", "git", "gh"]
---

# eval-search — lark-cli 搜索能力评测 Harness

**CRITICAL — 开始前 MUST 先用 Read 工具读取 [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md)（认证）和 [`RUBRIC.md`](RUBRIC.md)（评分细则）。**

## 目标

给 AI agent 一个自然语言搜索问题，它能否通过 lark-cli 在飞书企业知识库里找到正确答案？当它做不到，定位到：
- **(a) tool_capability** — 工具能力缺口（缺 shortcut / 缺 flag / 输出难解析）
- **(b) search_strategy** — agent 应该但没做的搜索动作
- **(c) skill_prompts** — 方法论没在 skill 文档里

并把归因汇聚成可执行的 PR 草稿。

## 适用场景

- "跑一轮搜索评测"
- "新人想参与 lark-cli 优化，从哪里开始"
- "对比一下最近改动对搜索效果的影响"
- "看看上一轮评测还有哪些归因没处理"

## 四个入口命令

```
/eval-search cycle [--loader-profile NAME] [--executor-profile NAME] [--subset N] [--report-doc URL]
                                      # 一键闭环：run → 打分/report → propose-pr，并把阶段进展写入云文档
/eval-search run [--loader-profile NAME] [--executor-profile NAME] [--subset N]
                                      # 跑一轮评测，产出 run-id。默认全量；--subset=3 抽样冒烟
/eval-search run --snapshot-only      # 只把评测集拉成本地 dataset.jsonl，供移除权限后复用
/eval-search propose-pr <run-id>     # 基于 run 生成 PR 草稿（含 before/after + 泛化声明 + regression 告警）
/eval-search report <run-id>         # 读已有 run 的 summary.json
```

新人典型流程优先使用 `cycle`，只有调试单个阶段时才手动执行 `run` / `report` / `propose-pr`。

## `/eval-search cycle` 上层闭环

详细步骤见 [`references/cycle.md`](references/cycle.md)。概要：

1. **初始化 cycle**：生成 `cycle-id` / `run-id`，创建 `tests/eval-search/runs/<run-id>/cycle.json`
2. **创建或绑定云文档**：若未传 `--report-doc`，用 `lark-cli docs +create --api-version v2 --doc-format markdown` 创建报告文档；若已传文档，则直接追加本轮章节
3. **阶段化执行并记录**：内部串联 `run → score/report → propose-pr`，每个阶段开始、成功、失败都先写本地 `cycle.json`，再追加到云文档
4. **产物归档**：云文档只写阶段状态、分数摘要、finding 摘要、PR URL、失败原因和本地产物路径；不得写标准答案、完整 trajectory、source_urls 或 key_error_snippets
5. **污染控制**：cycle 生成或使用的云文档默认是评测过程材料，必须记录为 tainted/process material；未来持久 blocklist 变更需要单独 PR，不得混入搜索效果优化 PR
6. **完成定义**：未传 `--skip-pr` 时，最终回复必须同时给出 Cloud report URL 和 Draft PR URL；任一链接缺失都不能视为完成

## 三层架构（必须隔离，违反会让结果失真）

```
Executor (sub-agent, Task 工具)
  输入: query only               不知道: expected / rubric / source_urls
  工具: 仅 lark-cli
  产出: trajectory + answer
            ↓
Judge (主 agent 切 hat，时序隔离)
  输入: query + answer + expected + rubric
  产出: 4 维打分 + 三桶 improvement
            ↓
Optimizer (sub-agent, Task 工具)
  输入: 全部 verdicts summary + 失败 case 的关键错误片段（不喂 trajectory 全文）
  产出: diff + 泛化声明字段
```

**隔离纪律**：
- Executor prompt 永远只注入 `query`，绝不传 expected/rubric/source_urls（盲测）
- Judge 必须在 Executor 全部跑完之后开始，不得和 Executor 共享 tool-use 窗口
- Optimizer 只看 Judge 聚合出的 summary，**不喂 trajectory 原文全文**，只喂失败 case 的关键错误行（防过拟合 + 控 context）

## `/eval-search run` 流程

详细步骤见 [`references/run-layout.md`](references/run-layout.md)。概要：

1. **确定性 setup**：先运行 `node --experimental-strip-types tests/eval-search/eval-search-run.ts --loader-profile <base-reader> --executor-profile <blind-runner> [--subset N]`。脚本会生成 run-id，建目录 `tests/eval-search/runs/<run-id>/`，并完成第 2-4 步。若只有一个账号，可先用 `--snapshot-only` 拉本地 `dataset.jsonl`，移除该账号的评测 Base 权限后，再用 `--dataset-file <snapshot-run>/dataset.jsonl` 继续
2. **拉数据集**：按 [`references/dataset.md`](references/dataset.md) 用 loader profile 从评测 base 拉最新数据 → `dataset.jsonl`
3. **账号隔离**：按 [`references/pollution-preflight.md`](references/pollution-preflight.md) 检查 executor profile 不在 `excluded_user_ids`，并主动探测 executor 不能读取评测 Base；若能读取则阻断
4. **污染预检**：用 executor profile 对每条 query 跑一次 `drive +search`，命中 [`references/known-tainted-tokens.md`](references/known-tainted-tokens.md) 里的 token 则标记 `contamination_risk`。只标记不阻断；Judge 阶段再决定是否扣分
5. **Executor 并行**：用 Task 工具启动 sub-agent 按 [`prompts/executor.md`](prompts/executor.md) 跑全部 case。每个 case trajectory 落盘 `trajectories/<case_id>.json`
6. **Judge 逐 case**：主 agent 按 [`prompts/judge.md`](prompts/judge.md) 打分，写 `verdicts.json`
7. **聚合**：按"改动落点文件"对 improvements 聚类，写 `summary.json`；输出 run-id 给用户

## `/eval-search propose-pr` 流程

详细见 [`references/pr-generation.md`](references/pr-generation.md)。概要：

1. **Optimizer 生成 diff**：用 Task 工具启动 sub-agent 按 [`prompts/optimizer.md`](prompts/optimizer.md) 读 summary + 两个仓库代码，产出 **cli diff + open diff（如有）** 和泛化声明
2. **应用 diff 到两个 worktree**：
   - cli 仓库：独立分支 `eval-search/auto-pr/<run-id>`
   - open 仓库（若有改动）：独立分支 `eval-search/auto-pr/<run-id>`，互不污染 main
3. **Quality gate**（当前仅 cli 仓库）：`make unit-test` + `golangci-lint run --new-from-rev=origin/main` 必须通过。失败 → Optimizer 最多迭代 2 次，仍失败 → 把触发失败的改动降级为 GitHub issue，不进 PR。open 仓库暂不跑 gate（CI 配置非 harness 可控）
4. **确定性 regression 重跑**：按 diff 之上重跑完整评测（复用 `/eval-search run` 内部流程），产出 after verdicts。**这一步不给 Optimizer 参与**
5. **组装两份 PR description**：按 [`references/pr-generation.md`](references/pr-generation.md) 里的模板，包含 before/after 数值、wins/regressions 逐 case 列表、泛化声明、未处理归因、**对端 PR 互相 link**
6. **`gh pr create --draft`**：双 PR 独立提，**独立 review、独立 merge**。不强绑定联动。一个 PR 先 merge 另一个还没 merge 也 OK，在 PR description 里标记 cross-ref

## 权限边界（v0.1 软约束，迭代中调整）

### PR 颗粒度

每个 `/eval-search propose-pr` 只能落一个主归因桶 / 一个改动主题。主 agent 在 apply diff 前必须复查 touched files，并按以下规则拆分：
- `search_strategy` / `skill_prompts`：只能提交搜索策略或 skill 文档优化 PR，例如 `skills/lark-drive/references/*-search.md` 或当前主搜索入口对应文档。不得混入 harness、runner、package、评测集、打分脚本或基础设施改动；不要给已进入维护期的 `docs +search` 新增策略依赖。
- `tool_capability`：只能提交 CLI shortcut / open converter 能力 PR。不得混入搜索策略文档，除非同一能力改动必须同步更新对应使用说明。
- `eval_harness` / 评测流程自身：必须独立 PR，不能和任何搜索效果优化 PR 混在一起。

### cli 仓库（`larksuite/cli`，当前目录）

Optimizer 默认允许改：
- `skills/**/*.md`
- 新增 `shortcuts/<new-or-existing-domain>/*.go` 及对应测试

Optimizer 不自动改：
- `internal/**`, `extension/**`, `cmd/root.go`, `cmd/service/**` 等基础设施 → 降级为 issue
- 任何旧 shortcut 的删除 / 重命名 / 破坏性改动

### open 仓库（`$GOPATH/src/code.byted.org/lark_as/open/`）

详见 [`references/open-repo-layout.md`](references/open-repo-layout.md)。简要：

Optimizer 默认允许改：
- `biz/search_open/entity/{name}.go` 的 `BuildDisplayInfo` / `BuildResponseItem` bug fix / `Prune` 及配套 `*_test.go`

Optimizer 不自动改：
- IDL（在独立的 `lark/idl` 仓库，需要跑 overpass，不属于 PR 范畴）
- `api_meta/**/*.yml`（契约变更，走人工）
- `biz/search_open/handler.go` / `adapter.go` / `pagetoken.go` / `response.go` 等基础设施
- 任何"新增 OAPI 字段"类需求（跨两个仓库 + 手工步骤，产出 issue 正文即可）

### 违反白名单的处理

Optimizer 把该 finding 写进 PR description 的"未处理归因"段（含建议 issue 正文），由新人创建对应 GitHub issue。**不发**跨仓库 / 超出白名单的 PR。

## 关键纪律（不遵守分数会失真）

1. **盲测纪律**：Executor prompt 只注入 `query`。即使主 agent fallback 接管 Executor，也必须自我约束不读 `dataset.jsonl` 的非 query 字段
2. **三层隔离**：Judge 不能和 Executor 在同一轮 reasoning；Optimizer 不喂 trajectory 全文
3. **Regression 软告警**：after 出现 regression 不硬 block，但必须在 PR description 里逐 case 列出；reviewer 判断
4. **泛化声明必填**：Optimizer 必须区分"针对具体 case 的改动"和"泛化原则性改动"。前者过拟合风险高，reviewer 重点看
5. **污染隔离**：harness 至少使用两个 profile。loader profile 可以读取评测 Base，但只允许用于拉数据集；executor profile 必须是专用测试账号（非 PM 账号、非 dataset owner 账号），且不能读取评测 Base。若 executor profile 的 `userOpenId` 出现在 [`references/known-tainted-tokens.md`](references/known-tainted-tokens.md) 的 `excluded_user_ids` 列表里，或 executor 可以读取评测 Base，拒绝启动

## 参考

- [`RUBRIC.md`](RUBRIC.md) — 4 维度评分细则
- [`prompts/executor.md`](prompts/executor.md) — Executor sub-agent 模板
- [`prompts/judge.md`](prompts/judge.md) — Judge 打分模板
- [`prompts/optimizer.md`](prompts/optimizer.md) — Optimizer PR 生成模板
- [`references/cycle.md`](references/cycle.md) — 一键闭环 + 云文档阶段日志
- [`references/dataset.md`](references/dataset.md) — 评测集 schema + 拉取方式
- [`references/pollution-preflight.md`](references/pollution-preflight.md) — 污染预检规则
- [`references/known-tainted-tokens.md`](references/known-tainted-tokens.md) — 已知泄露文档标记清单
- [`references/run-layout.md`](references/run-layout.md) — run 目录结构 + 中间产物约定
- [`references/pr-generation.md`](references/pr-generation.md) — PR 生成流程 + description 模板（双 PR）
- [`references/open-repo-layout.md`](references/open-repo-layout.md) — `lark_as/open` 仓库允许改动的白名单导航

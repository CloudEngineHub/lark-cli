# 已知污染文档标记清单

**维护原则**：只加，不删（除非文档被彻底销毁）。每次 v_n 迭代中新增的"评测过程记录"文档都要补进来。

## excluded_user_ids（必须排除的登录账号）

运行 harness 前，`lark-cli auth status.userOpenId` 若命中以下之一，harness 拒绝启动：

```yaml
excluded_user_ids:
  - ou_6927671c80c467507b88fae9a2983bdb   # 贾洪楠（搜索负责人 / dataset owner，有 base 读权限）
  # 补充规则：dataset owner、PR reviewer 的个人账号、任何能读 dataset base 的账号
```

## tainted_tokens（搜索命中这些 token 即标记 contamination_risk）

```yaml
tainted_tokens:
  # 评测集 base 本身（搜索很容易命中 base 里的 query 文本）
  - OOoEbNWhcaFOdisXDW7c0lKtn4g

  # v1/v2 harness 迭代记录 docx（含全部方法论 + per-case 分数）
  - VdUKdAXjmo9vl8xq4FrczK6unct

  # v1/v2 迭代讨论 / 参考流程文档（wiki 节点，含 table）
  - UHFJwOFOCiRgXdkxCoMc1JHgn0c

  # 2026-04-30 run 命中的评测集 / 评测分析类文档
  - QOhZbPgqDaJ3MQsTkGWcPdZUnHe   # Agentic评测集数据汇总
  - ZWQHssFhPh8FG5tca48cNRP0npb   # 知识问答评测集（v251212）
  - CVMqwRPJBi2aJQkabpAcwwjoneh   # Untitled Base（评测集内容）
  - VrNLbOJRJacXPOsyqHEcHFlunjc   # 意图_改写评测集
  - RmYObZGsWaQbjSslgSxc0CT7nFh   # 2601-300Q评测集
  - XtKhwTZ7aii6CNkXHZdcHj1bnwh   # 20251222 评测 Case 分析
  - RTkDw2QD3igsEMkKCamcKSM9nTh   # openclaw-竞对评测
  - XGbnbQLy6ayTt6s9AG3cwugMn3c   # 追问评测集
  - MSMGsruM2hUvzHtA3MZcSgBOnMe   # 追问精简版
  - QB4GsP3jfhbToLtFANict4zqn6d   # 追问拆分实验15
  - Vex5sCIeAhVyirtKTjUcn63mnod   # 精简版追问_v2
  - GYkIbtnfeac5RJsjjMAceTqJn2c   # 图片理解开灰前评测

  # 2026-05-06 optimizer_after_v2 中已被 fetch 的残留评测/过程材料
  - R2V0dojm2oeW9jx493icfbQpnDb   # 融合场景 GSB 评测报告
  - C0O2dnfxWoGD4bxJxzmcZehgnTj   # 企业问答基线评测机评应用报告
  - Q19HdaFdMopYoBxOiuLcaPbMnB3   # 模型追问 PE 迭代记录
  - Dv4adguERoakkhxWUdRlVaQvg5E   # 20240729 case 分析
  - ZDs4dU2fzoDMxwx6PcBcmWhZndf   # Q1 基线 agentic 评测集 91Q
```

## 新增条目流程

1. 发现某个飞书文档在评测中被 fetch + 用来作答 → 该文档 tainted
2. 提取 token：
   - docx URL `https://xxx/docx/<token>` → `tainted_tokens` 加 `<token>`
   - wiki URL `https://xxx/wiki/<token>` → `tainted_tokens` 加 `<token>`；**另外**用 `lark-cli wiki +resolve-node --token <token>` 拿到真实 `obj_token`，也加进去
   - base URL 只加 base_token 即可
3. 提交 PR 改本文件（commit message: `chore(eval-search): mark tainted <token> - <reason>`）

## 执行侧处理规则

- Preflight 命中 tainted token 只标记风险，不阻断整轮评测。
- Executor/collector 不能因为命中本文件就隐藏结果；否则评测会被过滤规则美化，不能反映真实搜索行为。
- Executor/collector 必须把命中本文件的结果标为 `tainted=true` / `evidence_excluded=true`。这些结果可以出现在 observed search results 中，但不能进入 evidence candidates、fetch 队列、答案合成或 recall top-5 证据。
- Collector 应把命中的 token 写进 trajectory / raw evidence，保留 `tainted` 这类元数据，交给 Judge 按 RUBRIC 判定污染扣分。
- `verdicts.json` 里只对“fetch 过 tainted token 且答案受其影响”的 case 扣污染分；单纯 search 命中但未 fetch 的 case 不扣污染分，但可以作为污染风险记录。
- 新增 collector、shortcut 或搜索策略时，都要把本文件当作统一标记清单读取，避免各处散落 hard-coded 污染 token。

## 替代策略（推荐）

**不要在飞书上写"评测过程记录" / "v_n 比对分析"之类文档**。都写成本仓库 markdown：

- 评测流程/设计 → `skills/eval-search/**`（已就位）
- 某轮迭代分析 → `tests/eval-search/runs/<run-id>/*.md`（gitignored，本地查看）
- 发布用的 retrospective → PR description / GitHub wiki / release notes

这样根本不会污染飞书搜索语料，污染标记清单的维护压力也会逐渐下降。

## `/eval-search cycle` 的例外

如果用户明确要求把中间结果记录到云文档，允许使用 [`cycle.md`](cycle.md) 的云文档日志，但必须遵守：

1. 云文档只写阶段状态、分数摘要、finding 摘要、PR URL 和本地产物路径；不写标准答案、完整 trajectory、source_urls 或 key_error_snippets
2. 创建或绑定报告文档后，立刻把 doc token 写入本轮 `tests/eval-search/runs/<run-id>/cloud-doc/tainted_tokens.json`
3. 本 cycle 的 regression / after-run 必须把该 token 作为额外污染 token
4. 需要持久 blocklist 时，单独开 `chore(eval-search): blocklist cycle report <run-id>` PR；不得混进搜索策略或能力优化 PR

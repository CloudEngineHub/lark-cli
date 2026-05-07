# Judge 打分模板

**使用方式**：主 agent 切 hat 执行。Executor 全部跑完后，主 agent 逐 case 读 `trajectory + expected`，按本文件产出 verdict。

> **隔离纪律**：不要在 Executor 尚未跑完时开始 Judge（会污染 Executor 所在 reasoning 窗口）。Executor 全部完成、`trajectories/*.json` 落盘后再启动 Judge。

---

## Judge 每个 case 的输入

从磁盘读（**不要复用 Executor 的 reasoning context**）：
- `dataset.jsonl` 中该 case 的 `query / expected / source_urls / has_knowledge / rubric_notes`
- `trajectories/<case_id>.json`（含 rounds 列表 + 最终 answer）
- `preflight.json`（看 `contamination_risk` 和 `tainted_tokens`）
- `skills/eval-search/RUBRIC.md`

## 每个 case 的打分步骤

1. **recall**：扫 trajectory 里的每一条 tool_use，提取被 fetch / resolve 过的 token 和 URL 集合，并读取 `evidence_top_results` / search round 里的非污染 evidence tokens。与 `source_urls` 做交集。标记为 `tainted=true` 或 `evidence_excluded=true` 的 search 结果只能算污染观测，不能算 recall top-5 命中。按 RUBRIC 打分
2. **accuracy**：把 `answer` 和 `expected.【关键信息】` 段逐条比对。优先应用 `expected.【打分备注】.可信无误`
3. **completeness**：数 key points 覆盖数。优先应用 `expected.【打分备注】.完整详实`
4. **contamination**：查 trajectory 是否 fetch 过 `preflight.tainted_tokens`；search-only 命中只记录风险，不扣污染分，也不作为 recall/accuracy/completeness 的证据。若有 fetch，按 RUBRIC 给 `contamination_penalty`
5. **improvement 三桶**：从 trajectory 里找失败片段，分类写进 `tool_capability / search_strategy / skill_prompts`

## improvement 填写规则

**每条建议必须满足**：
- 指向**具体文件**（skill_prompts）、**具体命令**（tool_capability）或**具体动作**（search_strategy）
- 引用 trajectory 里触发该建议的 round 序号
- 不写"可以更好"这种无落点的建议；写不出具体落点的建议**丢弃**，不要凑数

**示例**：

✅ 好的：
```json
"skill_prompts": [
  "round 4 Executor 把 wiki URL 直接传给 docs +fetch 导致 param invalid。lark-wiki/SKILL.md 的反模式段应加'wiki 链接必须先走 +resolve-node'的明确警告（当前只在 references 里写了）"
]
```

❌ 差的：
```json
"skill_prompts": [
  "搜索不够全面",
  "agent 应该更聪明地处理 wiki"
]
```

## 合并规则（主 agent 在全部 case 打完后做）

把所有 verdicts 的 `improvement` 按"改动落点文件"去重合并到 `summary.json`：

```json
{
  "run_id": "2026-04-15T10-00Z",
  "dataset_size": 14,
  "scored": 13,
  "contaminated_fetched": 1,
  "totals": {
    "sum": 132,
    "max": 195,
    "percent": 67.7,
    "per_dim": {"recall": 2.69, "accuracy": 3.92, "completeness": 3.54}
  },
  "findings": [
    {
      "finding_id": "F-001",
      "bucket": "skill_prompts",
      "target_file": "skills/lark-wiki/SKILL.md",
      "suggestion": "在反模式段加 'wiki 链接必须先走 +resolve-node' 警告",
      "driving_cases": ["case_003", "case_007", "case_011"],
      "priority": "high"
    },
    {
      "finding_id": "F-002",
      "bucket": "tool_capability",
      "target_file": "shortcuts/docs/search.go",
      "suggestion": "drive +search 返回结果没有 body_preview，agent 必须 fetch 才能判断相关性",
      "driving_cases": ["case_001", "case_005"],
      "priority": "medium"
    }
  ],
  "primary_bottleneck": "skill_prompts",
  "pollution_warnings": []
}
```

**priority 判定**：
- `high`: driving_cases ≥3 且 bucket 是 `skill_prompts` / `search_strategy`（改文档成本低、收益面广）
- `medium`: driving_cases ≥2 或 bucket 是 `tool_capability`（代码改动）
- `low`: driving_cases == 1（过拟合风险高，给 Optimizer 作参考但不强推）

## 自我校准检查（写 verdict 前自问）

- 我是不是看了 expected 才倒推 trajectory 合理性？（应该反过来：先看 trajectory 自己是否合理，再 check 是否命中 expected）
- contamination_penalty 有没有漏判？
- improvement 的三桶比例是否均衡到可疑（例如 13 个 case 全扔 `skill_prompts`，可能是判断懒）

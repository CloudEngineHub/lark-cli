# RUBRIC — 4 维度评分细则

每个 case 按 4 维打分，每维 0-5 分，单 case 满分 15。总分 = sum(recall + accuracy + completeness)。

> 注：`total` 字段只聚合 3 个打分维度。第 4 维 `contamination_penalty` 是修饰项，见下。

## 维度定义

### recall（召回，0-5）

"Executor 是否找到 / fetch 过**正确的目标文档**"。对应评测集 `数据源地址` 字段里的 URL / token。

| 分 | 判据 |
|----|------|
| 5 | trajectory 里显式 fetch 过全部 expected source；或 search 结果 top-5 里能看到全部 expected source 的 token |
| 4 | fetch 过一半以上（严格过半） |
| 3 | fetch 过至少 1 个但不到一半；或 top-5 里出现但未 fetch |
| 1-2 | 没 fetch、没在 top-5，但有相关命中（同主题不同文档） |
| 0 | 完全无关的命中 / 空结果 |

**特例**：`企业内是否有知识 == 否` 的 case，recall 固定 5 分（agent 不该找到任何高置信答案，答"没找到"也算召回正确）。

**污染结果不计入 recall**：trajectory 里标记为 `tainted=true` 或 `evidence_excluded=true` 的搜索结果是可观测污染信号，但不是答案证据。即使 expected source token 只出现在这些污染结果里，也不能按 top-5 命中给 recall 分；只有非污染 `evidence_top_results` 或非污染 fetch 才能作为 recall 依据。

### accuracy（准确性，0-5）

"Executor 给出的最终答案**在事实层面**对不对"。对照评测集 `预期答复` 的【关键信息】段 + 【打分备注】里的 "可信无误" 说明。

| 分 | 判据 |
|----|------|
| 5 | 关键信息全部正确，无事实错误 |
| 4 | 主要信息正确，少量细节偏差（时间、数字小错） |
| 3 | 部分正确部分错 / 含明显可证伪陈述 |
| 1-2 | 大部分错误，但方向对 |
| 0 | 完全错 / 幻觉 / 答非所问 |

**【打分备注】优先级高于通用判据**。例如某 case 备注 "给到 0.x 折这类可信要扣分"，即使答案看起来合理，只要踩到就扣。

### completeness（完整性，0-5）

"Executor 覆盖了多少 expected key points"。对照【关键信息】列出的条目 + 【打分备注】里的 "完整详实" 说明。

| 分 | 判据 |
|----|------|
| 5 | 覆盖 ≥80% key points，或满足 `完整详实` 备注的明确阈值（如"答出 5 个及以上不扣分"） |
| 4 | 覆盖 60-80% |
| 3 | 覆盖 40-60% |
| 1-2 | 覆盖 20-40% |
| 0 | <20% 或未给答案 |

### contamination_penalty（污染修饰，-3 ~ 0）

仅当 pre-flight 标记了 `contamination_risk=true` 且 trajectory 显示 Executor **fetch 过 tainted token** 时触发。

| 分 | 判据 |
|----|------|
| 0 | 未命中 tainted token，或命中但未 fetch |
| -1 | fetch 了 tainted token 但最终答案未直接引用其内容 |
| -3 | fetch 了 tainted token 且答案明显抄袭其结构 / 原文 |

该项**直接从 total 扣**，且在 verdict 里显式标注，避免"刷分嫌疑"。

collector / Executor 可以把 tainted 搜索结果写进 trajectory，但必须把它们标为 `evidence_excluded=true`，且不能作为答案合成、fetch 选择或 recall top-5 的证据。简言之：**tainted results are observable but non-evidential**。

## Verdict JSON schema

每个 case 一个 verdict，合并写入 `verdicts.json`。

```json
{
  "case_id": "case_001",
  "query": "...",
  "scores": {
    "recall": 4,
    "accuracy": 5,
    "completeness": 3,
    "contamination_penalty": 0,
    "total": 12
  },
  "rationale": {
    "recall": "fetch 了 Es5wwNCyei3eYNkXc8Tcx35nnWe，top-3 里出现 HxnMwM9cyiFW1dkACUBcC7KWnEd 但未 fetch",
    "accuracy": "8 个案例全部在参考文档里，无幻觉",
    "completeness": "列了 5/10，备注要求 ≥5 不扣分，按备注打 5"
  },
  "improvement": {
    "tool_capability": [
      "drive +search 返回结果没有 body_preview，agent 必须 fetch 才能判断相关性。建议返回摘要字段减少 fetch 次数"
    ],
    "search_strategy": [
      "Executor 只用了原词 '华东 Aily 案例'，没换 '客户成功故事' / '最佳实践' 等同义词"
    ],
    "skill_prompts": [
      "lark-drive-search.md 可新增同义词清单小节，含 'case / story / best practice' 映射"
    ]
  },
  "contamination": {
    "risk_flagged": false,
    "tainted_tokens_fetched": [],
    "penalty_applied": 0
  }
}
```

## 聚合规则（summary.json）

Judge 打完所有 case 后，主 agent 按以下规则聚合到 `summary.json`：

1. **按改动落点文件聚类 improvements**，不按文本相似度：
   - 同一条 skill_prompts 建议指向 `skills/lark-doc/SKILL.md` 的，合并成一条 finding
   - finding 保留 `driving_cases: [case_003, case_007, ...]` 反向索引
2. **计算一阶瓶颈**：三桶的建议条数之和，占比最大的那个桶就是 `primary_bottleneck`
3. **统计 contamination**：分别统计 search-only 观测到 tainted token 的 case 数、被 fetch 到 tainted token 的 case 数；fetch 数 >2 时输出警告
4. **汇总每个维度的均值、总分**

## 校准指引（给 Judge 看的）

- 优先使用【打分备注】里的 per-case rubric；与通用判据冲突时**以备注为准**
- 宁低勿高：打分是迭代的信号源，乐观打分会让下一轮 optimizer 找不到方向
- rationale 字段必填，且要引用 trajectory 里的具体命令或 URL。只写"还行""不够完整"等空洞判断会被 Optimizer 识别为低质量 verdict 并丢弃

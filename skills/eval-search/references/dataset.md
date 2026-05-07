# 评测集 schema + 拉取方式

## 位置

评测集存在飞书多维表格（**live 数据源**，PM 持续更新）：

- base_token: `OOoEbNWhcaFOdisXDW7c0lKtn4g`
- table_id: `tblGWdc19tKFZC6K`
- view_id: `vewGToSnWl`
- URL: https://bytedance.larkoffice.com/base/OOoEbNWhcaFOdisXDW7c0lKtn4g?table=tblGWdc19tKFZC6K&view=vewGToSnWl

> **污染警告**：这个 base 本身会被 `drive +search` 命中。harness 必须把账号拆成两个 profile：loader profile 只用于读取这个 base 并生成 `dataset.jsonl`；executor profile 只用于盲测搜索，**不可**加入该 base 的查看权限，否则评测结果被自答污染。详见 [`pollution-preflight.md`](pollution-preflight.md)。

## 原始字段（字段 id → 含义）

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `query` | text | 自然语言问题；Executor 唯一可见输入 |
| `len` | number | 历史字段，忽略 |
| `企业内是否有知识` | single-select | `是` / `否`。`否` 意味着企业知识库里本来就没答案，Executor 应答"找不到"，recall 维度固定给 5 |
| `预期答复（机评文本）` | text | 含三段：【关键信息】/ 【辅助信息】/ 【打分备注】。Judge 独占使用，**Executor 不可见** |
| `数据源地址` | text（markdown 链接） | expected source URLs；Judge 独占使用，**Executor 不可见** |

## 拉取命令

推荐用确定性 setup runner 拉取并转换：

```bash
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --loader-profile <base-reader> \
  --executor-profile <blind-runner> \
  --subset 3
```

如果只有一个账号，可以拆成两步：

```bash
# 账号仍有评测 Base 权限时，只拉本地快照
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --snapshot-only \
  --loader-profile <base-reader>

# 移除该账号的评测 Base 权限后，从本地快照继续盲测 setup
node --experimental-strip-types tests/eval-search/eval-search-run.ts \
  --dataset-file tests/eval-search/runs/<snapshot-run>/dataset.jsonl \
  --executor-profile <blind-runner>
```

只看原始 Base 拉取时，用 loader profile 执行：

```bash
lark-cli --profile <base-reader> base +record-list \
  --as user \
  --base-token OOoEbNWhcaFOdisXDW7c0lKtn4g \
  --table-id tblGWdc19tKFZC6K \
  --view-id vewGToSnWl \
  --limit 100
```

返回形如：
```json
{
  "ok": true,
  "data": {
    "data": [ [value_of_query, value_of_len, ...], ... ],
    "field_id_list": ["fldh3DHP53", ...],
    "fields": ["query", "len", "企业内是否有知识", "预期答复（机评文本）", "数据源地址"],
    "record_id_list": ["recvg4qIXMSU6K", ...],
    "has_more": true
  }
}
```

若 `has_more=true`，用 `--offset` 翻页直到全部拉完。

## 转换为 harness 内部 schema

主 agent 把每一行转成一个 case 对象，拼成 `dataset.jsonl`（jsonl，一行一个 case）：

```json
{
  "case_id": "case_001",
  "record_id": "recvg4qIXMSU6K",
  "query": "华东客户有哪些 Aily 优秀使用案例",
  "has_knowledge": true,
  "expected": {
    "key_points": "【关键信息】的原文段",
    "aux_info": "【辅助信息】的原文段",
    "rubric_notes": {
      "类型说明": "开放问题",
      "可信无误": "不局限于ref，只要明确作为aily使用案例出现即算可信",
      "完整详实": "答出5个及以上不扣分",
      "结构清晰": "无",
      "语言表述": "无",
      "相关辅助": "无",
      "引用准确": "无"
    }
  },
  "source_urls": [
    "https://bytedance.larkoffice.com/wiki/HxnMwM9cyiFW1dkACUBcC7KWnEd",
    "https://bytedance.larkoffice.com/wiki/Es5wwNCyei3eYNkXc8Tcx35nnWe"
  ]
}
```

### 转换要点

1. **case_id 编号**：按 record_id 在返回里的顺序分配 `case_001, case_002, ...`。同一次 run 内稳定，跨 run 不保证（PM 在 base 里插新行会错位）。如需跨 run 追踪，用 `record_id`
2. **filter `企业内是否有知识`**：harness 同时支持 `是` 和 `否` 的 case；但**pilot 阶段建议只跑 `是` 的**（`否` case 判分逻辑更复杂，后续加）
3. **解析 `预期答复` 的三段**：
   - split 文本找 `【关键信息】` / `【辅助信息】` / 【打分备注】` 三个 heading
   - 【打分备注】段是嵌套 JSON，`json.loads` 解析到 `rubric_notes`
   - 解析失败的 case 标记 `parse_error: true`，跳过不评（写进 `summary.json.skipped`）
4. **解析 `数据源地址`**：正则提取 markdown 链接 `[text](url)` → `source_urls: [url, ...]`。非 URL 的纯文本（如提示语）忽略
5. **空 query 过滤**：`query` 字段为空或纯空白的记录跳过

## Pilot 样本：只跑前 3 条冒烟

`/eval-search run --subset 3` 只拉前 3 条 `是` 类 case 跑。用于：
- 第一次落地 harness，验证端到端能跑通
- auto-PR 流程的 dry-run（改完 skill 跑 3 条看趋势）

## 频率 / 数据漂移

PM 在 base 里编辑 case 是常态。harness 不做 snapshot 冻结（v0.1 范围外），每次 `run` 拉最新。

**代价**：v_n 和 v_{n+1} 的分数差会混入 dataset 变化。在 PR description 里强制标注 `dataset_size / first_run_of_records` 两个字段，reviewer 自己判断。

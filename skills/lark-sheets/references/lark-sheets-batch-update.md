# Lark Sheet Batch Update

## 写入边界 + 回读校验

`+batch-update` 把多次写入打包成单次请求，但每个子操作仍受编辑类任务硬性默认规则约束：

1. **目标 range 必须落在用户授权范围内**：除用户明示要修改的区域外，子操作禁止扩张到无关单元格 / 列 / Sheet。规划 range 时先确认每个子操作的边界。
2. **批次完成后必须回读校验**：整个 `+batch-update` 执行成功后，用 `+csv-get` 或 `+cells-get` 抽样回读受影响区域，至少校验 3-5 个代表性单元格（首 / 中 / 末），与 `doubao_code_interpreter` 预先计算的预期值对照。
3. **预期条数前置断言**：涉及"批量填充 N 行"或"对 M 个区域分别写入"时，先把 N、M 硬编码进代码，回读后断言实际等于预期；不一致就再发一轮 `+batch-update` 补齐，禁止交付半成品。

## 使用场景

写入。批量执行多个写入工具操作。将多个工具调用合并为一次请求，按顺序依次执行。适合需要连续执行多个写入操作的场景（如先修改结构再写入数据）。注意：不支持嵌套 batch_update。

**⚠️ 何时必须使用 `+batch-update`（硬性要求）**：
- 需要对**多个**不同区域执行 `+cells-{merge|unmerge}` 时（如按分组合并多列相同内容）
- 需要对**多个**不同区域执行 `+dim-resize` 时（如统一调整多列列宽或多行行高）
- 需要先插入行列再写入数据时（`+dim-{insert|delete|hide|unhide|freeze|group|ungroup}` + `+cells-set`）
- 需要对多个区域执行不同写入操作时（多次 `+cells-set` + `+cells-clear` 等组合）

当同一工具需要对多个区域重复调用时，**必须**改用 `+batch-update` 合并为单次请求。逐个调用会快速耗尽工具调用轮次上限（60R），导致任务无法完成。

## Shortcuts

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成。CLI 的 shortcut 拆分、Risk 分级、分组、flag 表是事实源；本节不要手维护。

| MCP tool | CLI shortcut | Risk | 分组 |
| --- | --- | --- | --- |
| `batch_update` | `+batch-update` | high-risk-write | 批量 |

## Flags

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成（包含从 base shortcut-flags 子表派生的 flag 信息）。本节不要手维护——改 base 表再 `npm run sync:tool-shortcut-map`。

### `+batch-update`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet 定位（与子操作的 sheet 定位独立） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet 定位（与子操作的 sheet 定位独立） |
| `--data` | 专有 | string + File + Stdin | 是 | JSON：`{"operations":[{"tool":"set_cell_range","params":{...}}, ...]}`；按数组顺序串行执行 |
| `--yes` | 系统 | bool | 是 | `high-risk-write`，必须二次确认（不带时退出码 10） |
| `--dry-run` | 系统 | bool | 否 | 输出每个子操作的请求模板，零网络副作用 |

## Schemas

> 复合 JSON flag（`--data` / `--style` / `--options` / `--sort-keys`）的字段速查：只列顶层字段 + 一层嵌套结构。深层结构看 `## Examples` 段的真实示例；要拿完整 JSON Schema 跑 `lark-cli sheets <shortcut> --print-schema --flag <name>`（runtime introspection，待落地）。

### `+batch-update` `--data`

_要批量执行的操作列表，按顺序依次执行_

**数组项**（类型 object）：
- `input` (object) — 对应工具的入参，结构与单独调用该工具时完全一致
- `tool_name` (string) — 要执行的工具名称，如 "set_cell_range"、"clear_cell_range"、"modify_sheet_structure" 等

## Examples

> shortcut 拆分 / Risk / 分组 / flag 表都由 [`tool-shortcut-map.json`](../../tool-shortcut-map.json) 自动注入到上方 `## Shortcuts` / `## Flags` 段。本节只承载手维护补充：命令示例、Validate / DryRun / Execute 约束。

公共四件套：`--url` / `--spreadsheet-token` / `--sheet-id` / `--sheet-name`（前两者 XOR；`+batch-update` 本身不强制 sheet-id，子操作各自携带）。

### `+batch-update`

示例：

```bash
lark-cli sheets +batch-update --url "https://example.feishu.cn/sheets/shtXXX" --yes \
  --data @ops.json

# ops.json:
# {
#   "operations": [
#     {"tool": "modify_sheet_structure", "params": {"sheet_id":"...","operation":"insert","dimension":"row","start":10,"end":12}},
#     {"tool": "set_cell_range",          "params": {"sheet_id":"...","range":"A11:B12","values":[["a","b"],["c","d"]]}}
#   ]
# }
```

### Validate / DryRun / Execute 约束

- `Validate`：`--data` 必须合法 JSON，且 `operations` 是非空数组；逐个子操作 `tool` / `params.sheet_id` 字段必填校验；**禁止嵌套 batch_update**。
- `DryRun`：按顺序输出每个子操作的目标 API + 请求 body 模板；首个失败则整批 fail-fast（不实际执行任何后续）。
- `Execute`：按声明顺序串行执行；任一子操作失败立即中断并回滚到该子操作前状态（具体回滚能力取决于子操作类型，沿用 MCP `batch_update` 的语义）。

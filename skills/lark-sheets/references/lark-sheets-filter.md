# Lark Sheet Filter

## 真对象硬约束 + 数量校验

1. **真对象**：当用户要求"筛选 / 只看 / 仅保留 X"时，**必须**通过 `+filter-{create|update|delete}` 创建真实的筛选器对象。**禁止**用"删除不符合条件的行" / "新建子表只放符合条件的行" / 用 `+cells-set` 覆盖原表来代替——这些做法会让原数据丢失或不可恢复。
2. **筛选数量必校**：执行筛选后**必须**回读，断言 `len(visible_rows) == expected_count`。`expected_count` 来自先用 `doubao_code_interpreter` 在源数据上独立复现该筛选条件得到的结果数。两者不一致时禁止交付，需排查筛选条件 / 数据列类型问题。
3. **混合文本列禁止字面比较**：筛选 key 是公式文本（如 `1000+200=1200`）或带单位的混合文本时，先在辅助列里抽出纯数值再筛选；不能直接用文本比较。

## 使用场景

读写筛选器对象。本 Skill 包含两个工具：

| 操作需求 | 使用工具 | 说明 |
|---------|---------|------|
| 查看已有筛选器 | `+filter-list` | 获取筛选器的范围、规则和条件配置 |
| 创建/更新/删除筛选器 | `+filter-{create|update|delete}` | 对筛选器执行写入操作 |

典型工作流：先读取现有筛选器了解配置 → 执行创建/更新/删除 → **必须再次读取验证结果**。

**只读场景例外**：用户只是想知道哪些数据满足条件、并不要求修改表格展示时，可以走 `lark-sheets-read-data` 读后文本回答，不必创建筛选器。

**常见配置错误（必须注意）**：
- **筛选范围必须覆盖表头行**：筛选器的 range 必须从表头行开始（如 `A1:F100`），不能只包含数据行。缺少表头会导致筛选条件无法正确匹配列
- **更新已有筛选器前先读取**：如果子表上已存在筛选器，直接创建会报错或覆盖原有配置。应先用 `+filter-list` 查看是否存在筛选器，存在时使用 update 而非 create
- **筛选条件的列索引要精确**：筛选条件中的列标识必须与实际数据列精确对应，不要凭猜测填写
- **”调整筛选逻辑”要先读旧配置**：用户说”调整筛选”时，先读取现有筛选器的完整配置，理解当前规则后再修改，不要从零创建
- **创建后必须验证**：调用 `+filter-list` 确认筛选器配置正确且生效
- **筛选不支持正则表达式**：飞书表格筛选器不支持正则表达式，传入正则会当成普通文本处理。

## Shortcuts

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成。CLI 的 shortcut 拆分、Risk 分级、分组、flag 表是事实源；本节不要手维护。

| MCP tool | CLI shortcut | Risk | 分组 |
| --- | --- | --- | --- |
| `get_filter_objects` | `+filter-list` | read | 对象 |
| `manage_filter_object` | `+filter-create` | write | 对象 |
|  | `+filter-update` | write | 对象 |
|  | `+filter-delete` | high-risk-write | 对象 |

## Flags

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成（包含从 base shortcut-flags 子表派生的 flag 信息）。本节不要手维护——改 base 表再 `npm run sync:tool-shortcut-map`。

### `+filter-list`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-create`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--range` | 专有 | string | 是 | 筛选范围，含表头行（如 `A1:F1000`） |
| `--data` | 专有 | string + File + Stdin | 否 | JSON：`{"conditions":[{"col":"B","filter_type":"multiValue","expected":["北京","上海"]}]}`；省略则只建空筛选 |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-update`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--data` | 专有 | string + File + Stdin | 是 | JSON：可改 `range` 或追加 / 替换 `conditions[]`；先 `+filter-list` 回读再 patch |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-delete`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--yes` | 系统 | bool | 是 | `high-risk-write`，删除不可逆 |
| `--dry-run` | 系统 | bool | 否 |  |

## Schemas

> 复合 JSON flag（`--data` / `--style` / `--options` / `--sort-keys`）的字段速查：只列顶层字段 + 一层嵌套结构。深层结构看 `## Examples` 段的真实示例；要拿完整 JSON Schema 跑 `lark-cli sheets <shortcut> --print-schema --flag <name>`（runtime introspection，待落地）。

### `+filter-create` `--data` / `+filter-update` `--data`

_创建/更新的筛选器属性_

**顶层字段**：
- `filtered_columns` (array<string>?) — 可选
- `range` (string) — 筛选对象作用的单元格范围（A1 表示法）
- `rules` (array<object>) — 列级筛选规则列表，每一项对应一个具体列的筛选条件 each: { column_index: string, conditions: array<oneOf>, filtered_rows?: array<number> }

## Examples

> shortcut 拆分 / Risk / 分组 / flag 表都由 [`tool-shortcut-map.json`](../../tool-shortcut-map.json) 自动注入到上方 `## Shortcuts` / `## Flags` 段。本节只承载手维护补充：命令示例、Validate / DryRun / Execute 约束。

公共四件套：所有 shortcut 顶部排列 `--url` / `--spreadsheet-token` / `--sheet-id` / `--sheet-name`（XOR）。`filter_id` 等同于 `sheet_id`（每个工作表至多一个筛选器）。

### `+filter-list`

### `+filter-create`

```bash
lark-cli sheets +filter-create --url "..." --sheet-id "$SID" \
  --range "A1:F1000" \
  --data '{"conditions":[{"col":"B","filter_type":"multiValue","expected":["北京","上海"]}]}'
```

### `+filter-update`

> ⚠️ update 是覆盖式：传 `conditions` 会用整组新条件替换旧组。如只想加一条，要带上已有的全部条件再追加。

### `+filter-delete`

### Validate / DryRun / Execute 约束

- `Validate`：XOR 公共四件套；`+filter-create` 校验 `--range` 至少 2 行（表头 + 至少 1 行数据）；`+filter-update` 必须先 `+filter-list` 确认目标存在；`+filter-delete` 强制 `--yes` 或 `--dry-run`。
- `DryRun`：输出"将要 POST/PATCH/DELETE 的 filter 请求模板"。
- `Execute`：写后调用 `+filter-list` 回读，envelope.meta.verification 给出当前筛选条件 + 已过滤行数。

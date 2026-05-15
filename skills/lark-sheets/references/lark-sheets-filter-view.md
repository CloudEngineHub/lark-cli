# Lark Sheet Filter View

## 概念回顾

筛选视图是 sheet 内的多份独立筛选配置，每个视图持有自己的 `range` 和 `rules`，由独立 `view_id`（10 位随机字符串）标识。一个 sheet 可有多个视图，视图的隐藏行仅在用户进入该视图时本地生效，不影响其他协作者，也不与该 sheet 上可能并存的筛选器（filter）互相影响。

`+filter-{view-create|view-update|view-delete}` 负责视图本身的 CRUD（create / update / delete）；视图的"进入 / 退出"（激活态）是本地状态，不在工具语义内。

## 使用场景

读写筛选视图对象。本 Skill 包含两个工具：

| 操作需求 | 使用工具 | 说明 |
|---------|---------|------|
| 查看已有筛选视图 | `+filter-view-list` | 获取 sheet 上所有视图（视图名、范围、规则） |
| 创建 / 更新 / 删除筛选视图 | `+filter-{view-create|view-update|view-delete}` | 3 种 operation：create / update / delete |

典型工作流：先读取现有视图了解配置 → 执行创建 / 更新 / 删除 → **必须再次读取验证结果**。

**常见配置错误（必须注意）**：
- **视图范围必须覆盖表头行**：视图的 range 必须从表头行开始（如 `A1:F100`），不能只包含数据行
- **更新前先读取**：用户说"调整这个视图"时，先用 `+filter-view-list` 拉到目标视图当前 rules，**只改差异列**再回写
- **多次 create 不能复用 view_id**：复用应走 `update`，重复 `create` 会产生新视图
- **筛选不支持正则表达式**：飞书表格筛选器不支持正则表达式，传入正则会当成普通文本处理

## Shortcuts

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成。CLI 的 shortcut 拆分、Risk 分级、分组、flag 表是事实源；本节不要手维护。

| MCP tool | CLI shortcut | Risk | 分组 |
| --- | --- | --- | --- |
| `get_filter_view_objects` | `+filter-view-list` | read | 对象 |
| `manage_filter_view_object` | `+filter-view-create` | write | 对象 |
|  | `+filter-view-update` | write | 对象 |
|  | `+filter-view-delete` | high-risk-write | 对象 |

## Flags

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成（包含从 base shortcut-flags 子表派生的 flag 信息）。本节不要手维护——改 base 表再 `npm run sync:tool-shortcut-map`。

### `+filter-view-list`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--view-id` | 专有 | string | 否 | 按筛选视图 reference_id 过滤（命中即只返回单个视图） |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-view-create`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--data` | 专有 | string + File + Stdin | 是 | 视图配置 JSON：`{"view_name":"...","range":"A1:Z100","rules":[...]}`；省略 view_id 表示 create；range 必填且必须覆盖表头行 |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-view-update`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--view-id` | 专有 | string | 是 | 目标视图 reference_id |
| `--data` | 专有 | string + File + Stdin | 是 | 部分更新 JSON：含 view_name / range / rules 之一即可；先 +filter-view-list 回读再 patch |
| `--dry-run` | 系统 | bool | 否 |  |

### `+filter-view-delete`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--view-id` | 专有 | string | 是 | 目标视图 reference_id |
| `--yes` | 系统 | bool | 是 | `high-risk-write`，必须二次确认（不带时退出码 10） |
| `--dry-run` | 系统 | bool | 否 |  |

## Schemas

> 复合 JSON flag（`--data` / `--style` / `--options` / `--sort-keys`）的字段速查：只列顶层字段 + 一层嵌套结构。深层结构看 `## Examples` 段的真实示例；要拿完整 JSON Schema 跑 `lark-cli sheets <shortcut> --print-schema --flag <name>`（runtime introspection，待落地）。

### `+filter-view-create` `--data` / `+filter-view-update` `--data`

_create / update 的视图属性_

**顶层字段**：
- `filtered_columns` (array<string>?) — 可选
- `range` (string?) — 视图作用的单元格范围（A1 表示法）
- `rules` (array<object>?) — 列级筛选规则列表，每一项对应一个具体列的筛选条件 each: { column_index: string, conditions: array<oneOf>, filtered_rows?: array<number> }
- `view_name` (string?) — 可选

## Examples

> shortcut 拆分 / Risk / 分组 / flag 表都由 [`tool-shortcut-map.json`](../../tool-shortcut-map.json) 自动注入到上方 `## Shortcuts` / `## Flags` 段。本节只承载手维护补充：命令示例、Validate / DryRun / Execute 约束。

> ⚠️ 本 skill 是 **CLI 独有**（meta `surface: cli-only`）；`generate_mcp` 跳过，不会进 sheet-ai-skills SKILL 集。AI/MCP 侧暂不暴露筛选视图能力。

公共四件套：所有 shortcut 顶部排列 `--url` / `--spreadsheet-token` / `--sheet-id` / `--sheet-name`（XOR）。`view_id` 是 10 位随机字符串，每个 sheet 可有多个视图。

### `+filter-view-list`

```bash
# 列出某个 sheet 的全部筛选视图
lark-cli sheets +filter-view-list --url "..." --sheet-id "$SID"

# 按 view_id 精确定位
lark-cli sheets +filter-view-list --url "..." --sheet-id "$SID" --view-id vAbcde1234
```

### `+filter-view-create`

```bash
lark-cli sheets +filter-view-create --url "..." --sheet-id "$SID" \
  --data '{
    "view_name": "活跃用户",
    "range": "A1:F1000",
    "rules": [
      {"col": "C", "filter_type": "number", "compare": "greater", "expected": [100]}
    ]
  }'
```

> `range` **必须覆盖表头行**（如 `A1:F1000`），不能只包含数据行；`view_name` 重名时服务端自动改名。

### `+filter-view-update`

> ⚠️ update 是 patch：传 `view_name` / `range` / `rules` 任意一个或多个；先 `+filter-view-list` 读取当前 rules 再回写差异。重复 `+filter-view-create` 不会复用 view_id，会产生新视图。

### `+filter-view-delete`

> ⚠️ 视图删除不可逆；视图不存在按幂等成功处理。先 `--dry-run` 看 view_id 确认。

### Validate / DryRun / Execute 约束

- `Validate`：XOR 公共四件套；`+filter-view-create` 校验 `--data.range` 起始行为表头（第一行）；`+filter-view-update` 必须先 `+filter-view-list` 确认 view 存在；`+filter-view-delete` 强制 `--yes` 或 `--dry-run`。
- `DryRun`：输出"将要 POST/PATCH/DELETE 的 view 请求模板"，零网络副作用；`--sheet-name` 在 dry-run 输出里生成为 `<resolve:Sheet1>` 占位符。
- `Execute`：写后调用 `+filter-view-list --view-id <new>` 回读，envelope.meta.verification 给出当前 range + rules 与请求体的对比。

# Lark Sheet Float Image

> **单元格图片 vs 浮动图片**：飞书表格有两种图片类型，请根据需求选择正确的工具：
> - **单元格图片**：图片嵌入在单元格内部，随单元格移动，属于单元格内容的一部分。→ 使用 `+cells-set`，在 `rich_text` 中设置 `type: "embed-image"`（见 lark_sheet_write_cells Skill）。
> - **浮动图片**（本 Skill）：图片悬浮在单元格上方，可自由指定位置、大小和层级，不属于任何单元格的内容。→ 使用本 Skill 的 `+float-{image-create|image-update|image-delete}`。

## 真对象硬约束

当用户要求"插入图片 / 添加 logo / 放一张图"时，**必须**通过 `+float-{image-create|image-update|image-delete}`（浮动图片）或 `+cells-set` 的 `embed-image`（单元格图片）创建真实的图片对象。**禁止**只在文本回复中给出图片链接 / 描述图片内容代替插入。判断标准：交付后 `+float-image-list` 或单元格 `rich_text` 必须能读到该图片对象。

## 使用场景

读写**浮动图片**对象（悬浮在单元格上方的图片，不属于单元格内容）。本 Skill 包含两个工具：

| 操作需求 | 使用工具 | 说明 |
|---------|---------|------|
| 查看已有浮动图片 | `+float-image-list` | 获取浮动图片的位置、大小和层级配置 |
| 创建/更新/删除浮动图片 | `+float-{image-create|image-update|image-delete}` | 对浮动图片执行写入操作 |

典型工作流：先读取现有浮动图片了解配置 → 执行创建/更新/删除 → **必须再次读取验证结果**。

**常见配置错误（必须注意）**：
- **单元格图片 vs 浮动图片选择错误**：如果用户希望图片嵌入单元格内部（随单元格移动），应使用 `+cells-set` 的 `rich_text` + `embed-image`，而非本 Skill
- **图片位置参数要精确**：锚点单元格的行列索引和偏移量决定了图片位置，设置不当会导致图片遮挡数据
- **创建后必须验证**：调用 `+float-image-list` 确认图片位置和大小正确

reference_id 的映射规则：
- `image_uri`：`<|image|>:abcdef` 或者 `<|superscript|>:abcdef-<|image|>:abcdef`
- `float_image_id`：`<|float_image|>:abcdef`
其中 `abcdef` 为实际的对象 ID，占位符仅用于示意，不可直接使用。

`image_uri` 与 `image_token` 是「指定图片资源」的两种等价方式（与 `+cells-set` 中 `embed-image` 的语义一致）：
- `image_uri`：上传链路给到的图片 reference_id（如 `<|image|>:abcdef`），由系统自动转 fileToken
- `image_token`：图片 fileToken，常见来源是 `+float-image-list` 返回的 `image_token`（适合"换皮不换位置"等基于已有图片的复用场景）
- create 时二者必须有其一；update 时**仅在需要替换图片本身时**传入新的 `image_uri` 或 `image_token`，不传则保留原图。

## Shortcuts

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成。CLI 的 shortcut 拆分、Risk 分级、分组、flag 表是事实源；本节不要手维护。

| MCP tool | CLI shortcut | Risk | 分组 |
| --- | --- | --- | --- |
| `get_float_image_objects` | `+float-image-list` | read | 对象 |
| `manage_float_image_object` | `+float-image-create` | write | 对象 |
|  | `+float-image-update` | write | 对象 |
|  | `+float-image-delete` | high-risk-write | 对象 |

## Flags

> 由 [`tool-shortcut-map.json`](../../../canonical-spec/tool-shortcut-map.json) 自动生成（包含从 base shortcut-flags 子表派生的 flag 信息）。本节不要手维护——改 base 表再 `npm run sync:tool-shortcut-map`。

### `+float-image-list`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--float-image-id` | 专有 | string | 否 | 按 id 过滤；省略时列工作表全部 |
| `--dry-run` | 系统 | bool | 否 |  |

### `+float-image-create`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--data` | 专有 | string + File + Stdin | 是 | JSON：`{"image_uri":"...","image_name":"foo.png","position":{"row":2,"col":"D"},"size":{"width":300,"height":200},"offset":{"x":0,"y":0}}` |
| `--dry-run` | 系统 | bool | 否 |  |

### `+float-image-update`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--float-image-id` | 专有 | string | 是 | 目标图片 id |
| `--data` | 专有 | string + File + Stdin | 是 | 完整或足够完整的配置 JSON（先 `+float-image-list --float-image-id <id>` 回读再 patch） |
| `--dry-run` | 系统 | bool | 否 |  |

### `+float-image-delete`

| Flag | 分类 | Type | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `--url` | 公共 | string | XOR | spreadsheet URL（与 `--spreadsheet-token` 二选一） |
| `--spreadsheet-token` | 公共 | string | XOR | spreadsheet token（与 `--url` 二选一） |
| `--sheet-id` | 公共 | string | XOR | 工作表 reference_id（与 `--sheet-name` 二选一） |
| `--sheet-name` | 公共 | string | XOR | 工作表名称（与 `--sheet-id` 二选一） |
| `--float-image-id` | 专有 | string | 是 | 目标图片 id |
| `--yes` | 系统 | bool | 是 | `high-risk-write`，删除不可逆 |
| `--dry-run` | 系统 | bool | 否 |  |

## Schemas

> 复合 JSON flag（`--data` / `--style` / `--options` / `--sort-keys`）的字段速查：只列顶层字段 + 一层嵌套结构。深层结构看 `## Examples` 段的真实示例；要拿完整 JSON Schema 跑 `lark-cli sheets <shortcut> --print-schema --flag <name>`（runtime introspection，待落地）。

### `+float-image-create` `--data` / `+float-image-update` `--data`

_创建/更新的浮动图片属性_

**顶层字段**：
- `image_name` (string) — 图片名称，含拓展名，create 时必填
- `image_token` (string?) — 图片 fileToken（与 image_uri 二选一）
- `image_uri` (string?) — 图片的 reference_id（与 image_token 二选一）
- `offset` (object?) — 可选 { col_offset?: number, row_offset?: number }
- `position` (object) — 必填 { col: string, row: number }
- `size` (object) — 必填 { height: number, width: number }
- `z_index` (number?) — 可选

## Examples

> shortcut 拆分 / Risk / 分组 / flag 表都由 [`tool-shortcut-map.json`](../../tool-shortcut-map.json) 自动注入到上方 `## Shortcuts` / `## Flags` 段。本节只承载手维护补充：命令示例、Validate / DryRun / Execute 约束。

公共四件套：所有 shortcut 顶部排列 `--url` / `--spreadsheet-token` / `--sheet-id` / `--sheet-name`（XOR）。浮动图片是 sheet 级对象——和单元格内嵌图片不同（后者走 `+cells-set`）。

### `+float-image-list`

### `+float-image-create`

> `image_uri` 通常是先用 `upload_sheet_asset`（暂无 CLI shortcut，走 raw API）上传后拿到的 token，或者用 https URL（部分租户可直接引用）。

```bash
lark-cli sheets +float-image-create --url "..." --sheet-id "$SID" --data @img.json
```

### `+float-image-update`

### `+float-image-delete`

### Validate / DryRun / Execute 约束

- `Validate`：XOR 公共四件套；`+float-image-create` 校验 `--data.image_uri` 非空、`position` / `size` 合法；`+float-image-update` 必须 `--float-image-id`；`+float-image-delete` 强制 `--yes` 或 `--dry-run`。
- `DryRun`：写操作输出"将要 POST/PATCH/DELETE 的 float_image 请求模板"。
- `Execute`：写后调用 `+float-image-list --float-image-id <id>` 回读，envelope.meta.verification 给出新位置 / 尺寸对比。

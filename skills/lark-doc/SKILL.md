---
name: lark-doc
version: 2.0.0
description: "飞书云文档：创建和编辑飞书文档。默认使用 DocxXML 格式（也支持 Markdown）。创建文档、获取文档内容（支持 simple/with-ids/full 三种导出详细度，以及 full/outline/range/keyword/section 五种局部读取模式，可按目录、block id 区间、关键词或标题自动成节只拉部分内容以节省上下文）、更新文档（八种内容指令：str_replace/str_delete/block_insert_after/block_replace/block_delete/block_move_after/overwrite/append + 七种表格结构指令：table_insert_rows/table_insert_cols/table_delete_rows/table_delete_cols/table_merge_cells/table_unmerge_cells/table_update_property 用于插入/删除行列、合并/拆分单元格、设置表头/列宽、以及对单元格/整行/整列/A1 矩形区域设置背景色和垂直对齐）、上传和下载文档中的图片和文件、搜索云空间文档。当用户需要创建或编辑飞书文档、读取文档内容、对文档内嵌表格做结构或样式修改（加一行/合并 A1:C3/设置表头/给整行或整列加背景色/给矩形区域加背景色/调整列宽/设置单元格垂直对齐）、在文档中插入图片、搜索云空间文档时使用；如果用户是想按名称或关键词先定位电子表格、报表等云空间对象，也优先使用本 skill 的 docs +search 做资源发现。"
metadata:
  requires:
    bins: ["lark-cli"]
  cliHelp: "lark-cli docs --help"
---

# docs (v2)

> **⚠️ API 版本：本 skill 使用 v2 API。所有 `docs +create`、`docs +fetch`、`docs +update` 命令必须携带 `--api-version v2`。**

```bash
# 常用示例
lark-cli docs +fetch  --api-version v2 --doc "文档URL或token"
lark-cli docs +create --api-version v2 --content '<title>标题</title><p>内容</p>'
lark-cli docs +update --api-version v2 --doc "文档URL或token" --command append --content '<p>内容</p>'
```

## 前置条件 — 执行操作前必读

**CRITICAL — 执行对应操作前，MUST 先用 Read 工具读取以下文件，缺一不可：**
1. [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md) — 认证、权限处理、全局参数（所有操作通用）
2. **读取文档（`docs +fetch`）** → 必读 [`lark-doc-fetch.md`](references/lark-doc-fetch.md)（`--scope` / `--detail` 选择、局部读取策略、`<fragment>` / `<excerpt>` 输出结构）
3. **创建或编辑文档内容** → 必读 [`lark-doc-xml.md`](references/lark-doc-xml.md)（XML 语法规则，仅当用户明确要求 Markdown 时改读 [`lark-doc-md.md`](references/lark-doc-md.md)）；从零创建时加读 [`lark-doc-create-workflow.md`](references/style/lark-doc-create-workflow.md)；编辑已有文档时加读 [`lark-doc-update-workflow.md`](references/style/lark-doc-update-workflow.md)

**未读完以上文件就执行相应操作会导致参数选择错误、格式错误或样式不达标。**

> **格式选择规则（全局）：** `docs +create` 和 `docs +update` 始终使用 XML 格式（`--doc-format xml`，即默认值），除非用户明确要求使用 Markdown。XML 支持 callout、grid、checkbox 等丰富 block 类型——不要因为 Markdown 更简单就自行切换。

## 快速决策
- 用户需要在文档内**创建、复制或移动**资源块（画板、电子表格、多维表格等）时，必须先读取 [`lark-doc-xml.md`](references/lark-doc-xml.md) 的「三、资源块」章节
- 用户说"看一下文档里的图片/附件/素材""预览素材" → 用 `lark-cli docs +media-preview`
- 用户明确说"下载素材" → 用 `lark-cli docs +media-download`
- 如果目标是画板/whiteboard/画板缩略图 → 只能用 `lark-cli docs +media-download --type whiteboard`（不要用 `+media-preview`）
- 用户说"找一个表格""按名称搜电子表格""找报表""最近打开的表格" → 先用 `lark-cli docs +search` 做资源发现
- `docs +search` 不只搜文档/Wiki，结果里会直接返回 `SHEET` 等云空间对象
- 拿到 spreadsheet URL/token 后 → 切到 `lark-sheets` 做对象内部操作
- 用户说"给文档加评论""查看评论""回复评论""给评论加/删除表情 reaction" → 切到 `lark-drive` 处理
- 用户说"给表格添加一行""删除第二列""合并单元格""把 A1:C3 合并""拆分""设置表头""在表头加背景色""给第 2-3 行加背景色""给 B-C 两列加背景色""把 A1:C3 整块标成浅蓝""调整列宽""单元格/整行/整列垂直居中" → 使用 `table_*` 指令（见下方「文档内嵌表格操作」）
- **区分文档内嵌表格 vs 电子表格**：文档中的 `<table id="blkcnXXXX">` → `table_*` 指令；独立的电子表格 `<sheet>` / Spreadsheet → 切到 [`lark-sheets`](../lark-sheets/SKILL.md)
- 文档内容中出现嵌入的 `<sheet>`、`<bitable>` 或 `<cite file-type="sheets|bitable">` 标签时 → **必须主动提取 token 并切到对应技能下钻读取内部数据**，不能只呈现标签本身

| 标签 / 属性 | 提取字段 | 切到技能 |
|-|-|-|
| `<sheet token="..." sheet-id="...">` | `token` -> spreadsheet_token, `sheet-id` | [`lark-sheets`](../lark-sheets/SKILL.md) |
| `<bitable token="..." table-id="...">` | `token` -> app_token, `table-id` | [`lark-base`](../lark-base/SKILL.md) |
| `<cite type="doc" file-type="sheets" token="..." sheet-id="...">` | 同 `<sheet>` | [`lark-sheets`](../lark-sheets/SKILL.md) |
| `<cite type="doc" file-type="bitable" token="..." table-id="...">` | 同 `<bitable>` | [`lark-base`](../lark-base/SKILL.md) |
| `<synced_reference src-token="..." src-block-id="...">` | `src-token` -> doc_token, `src-block-id` -> block_id | 用 `docs +fetch` 读取 src-token 文档，定位 block |

**补充：** `docs +search` 也承担"先定位云空间对象，再切回对应业务 skill 操作"的资源发现入口角色；当用户口头说"表格/报表"时，也优先从这里开始。

## Shortcuts（推荐优先使用）

Shortcut 是对常用操作的高级封装（`lark-cli docs +<verb> [flags]`）。有 Shortcut 的操作优先使用。

| Shortcut | 说明 |
|----------|------|
| [`+search`](references/lark-doc-search.md) | Search Lark docs, Wiki, and spreadsheet files (Search v2: doc_wiki/search) |
| [`+create`](references/lark-doc-create.md) | Create a Lark document (XML / Markdown) |
| [`+fetch`](references/lark-doc-fetch.md) | Fetch Lark document content (XML / Markdown) |
| [`+update`](references/lark-doc-update.md) | Update a Lark document — content ops (str_replace / block_replace / ...) + [table ops](references/lark-doc-table-ops.md) (table_insert_rows / table_merge_cells / ...) |
| [`+media-insert`](references/lark-doc-media-insert.md) | Insert a local image or file at the end of a Lark document (4-step orchestration + auto-rollback) |
| [`+media-download`](references/lark-doc-media-download.md) | Download document media or whiteboard thumbnail (auto-detects extension) |
| [`+whiteboard-update`](../lark-whiteboard/references/lark-whiteboard-update.md) | Alias of `whiteboard +update`. Update an existing whiteboard with DSL, Mermaid or PlantUML. Prefer `whiteboard +update`; refer to lark-whiteboard skill for details. |

## 文档内嵌表格操作

表格相关的用户意图分三类——**先辨别意图，再选路径**，不要把三者混为一谈：

| 用户意图 | 使用路径 | 参考 |
|---|---|---|
| **创建新表格**（新建文档 / 在已有文档追加一张表） | `+create` 或 `+update --command append` / `block_insert_after` + `<table>` DocxXML | [`lark-doc-xml.md`](references/lark-doc-xml.md) 表格画廊 |
| **修改表格结构和样式**（加行删列、合并、表头、列宽、单元格背景色/对齐） | `docs +update --command table_*` | [`lark-doc-table-ops.md`](references/lark-doc-table-ops.md) |
| **修改单元格文字** | `docs +update --command block_replace` + 单元格 block ID | [`lark-doc-update.md`](references/lark-doc-update.md) |

### 工作流（结构 / 样式）
1. `docs +fetch --api-version v2 --doc <url> --detail with-ids` → 在返回的 XML 中找到 `<table id="blkcnXXXX">` 提取 table block ID
2. `docs +update --api-version v2 --doc <url> --command table_* --table-block-id blkcnXXXX ...`
3. 需要时用 `docs +fetch` 验证结果

### 指令速查

协议统一使用 **1-based** 索引（与 A1 记法对齐），`0` / `-1` 是特殊哨兵值。详见 [`lark-doc-table-ops.md`](references/lark-doc-table-ops.md) 的「索引约定速查」。

| 指令 | 用途 | 关键参数 |
|------|------|----------|
| `table_insert_rows` | 插入行 | `--row-index`（1-based；`0`=首行之前，`-1`=末尾追加） |
| `table_insert_cols` | 插入列 | `--col`（字母；`0`=首列前，`-1`=末尾） |
| `table_delete_rows` | 删除行范围 | `--row-start`, `--row-end`（**1-based** 左闭右开） |
| `table_delete_cols` | 删除列范围 | `--col-start`, `--col-end`（字母，**1-based 左闭右开** —— `--col-end` 不含；与 `table_delete_rows` 一致） |
| `table_merge_cells` | 合并单元格 | `--range`（A1:C3，两端都包含）；⚠️ 跨 thead/tbody 边界合并产生 `<th rowspan>` 并吃掉对应 body 单元格，详见参考文档 |
| `table_unmerge_cells` | 拆分单元格 | `--cell`（A1 记法） |
| `table_update_property` | 表级：不带任何定位 flag → `--col-width` / `--header-row` / `--header-column`。单元格级（`--background-color` / `--vertical-align`）支持四种**互斥**的定位模式：`--cell B3`（单格）、`--range A1:C3`（A1 矩形）、`--row-start/--row-end`（整行区间）、`--col-start/--col-end`（整列区间） | 见参考文档 |

所有 `table_*` 指令都需要 `--table-block-id`。单元格 / 区域用 A1 记法，列用字母（A/B/C），行号 1-based。

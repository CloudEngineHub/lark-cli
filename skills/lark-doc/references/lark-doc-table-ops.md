
# docs +update — 表格结构操作（table_* 指令）

> **前置条件：** 先阅读 [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) 了解认证、全局参数和安全规则。

对文档中**已有表格**进行结构性修改：插入/删除行列、合并/拆分单元格、设置表头和列宽、单元格样式。

> **修改单元格文本内容**仍用 `block_replace`，参见 [`lark-doc-update.md`](lark-doc-update.md)。
>
> **创建新表格**不用 `table_*` — 请用 `+create` 或 `+update --command append / block_insert_after` 直接写入 `<table>` XML，表格画廊见 [`lark-doc-xml.md`](lark-doc-xml.md)。

## 能力矩阵

`table_*` 指令只处理**结构和样式**，不处理**文字内容**。

| 能做 | 不能做 |
|------|--------|
| 插入/删除行列 | 复制整行到其他位置 |
| 合并/拆分单元格 | 交换两行位置 |
| 设置表头（首行/首列） | 修改单元格文字内容（→ `block_replace`） |
| 调整列宽 | 调整行高（暂未开放） |
| 单元格背景色、垂直对齐 | 单元格内字体样式（→ XML 内联标签） |

## 何时使用 table_* 指令

| 操作类型 | 使用指令 | 说明 |
|----------|----------|------|
| 插入/删除行或列 | `table_insert_rows` / `table_delete_cols` 等 | 结构性变更，~20 tokens/op |
| 合并/拆分单元格 | `table_merge_cells` / `table_unmerge_cells` | 结构性变更 |
| 设置表头、调整列宽 | `table_update_property`（表级模式） | 表级属性 |
| 单元格背景色、垂直对齐 | `table_update_property`（单元格模式） | 单元格样式 |
| **修改单元格文字** | **`block_replace`** | 内容变更，走原有流程 |
| **创建新表格** | **`append` / `block_insert_after`** + `<table>` XML | 见 [`lark-doc-xml.md`](lark-doc-xml.md) 表格画廊 |

## 工作流

### Step 1 — 获取表格 block ID

```bash
lark-cli docs +fetch --api-version v2 --doc "<url>" --detail with-ids
```

在返回的 XML 中找到 `<table id="blkcnXXXX">` 标签，提取 `id` 值即为 `--table-block-id`。

### Step 2 — 执行表格操作

```bash
# 示例：在表格末尾追加一行
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_rows \
  --table-block-id blkcnXXXX --row-index -1
```

### Step 3 — 验证（可选）

```bash
lark-cli docs +fetch --api-version v2 --doc "<url>" --detail with-ids
```

## 通用参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `--api-version` | 是 | 固定传 `v2` |
| `--doc` | 是 | 文档 URL 或 token |
| `--command` | 是 | `table_*` 指令名 |
| `--table-block-id` | 是 | 表格 block ID |
| `--revision-id` | 否 | 基准版本号，-1 = 最新（默认 `-1`） |

## 索引约定速查 ⚠️

**协议统一使用 1-based 索引**——与 A1 记法、`--row-start`/`--row-end` 保持一致。`0` 和 `-1` 是在 1-based 基础上的**特殊语义值**。

| 参数 | 正常取值 | 特殊值 |
|------|----------|--------|
| `--row-index`（insert） | 1-based 行号，`--row-index N` ⇒ 新行成为第 N 行（已有第 N 行及之后的行向下平移） | `0` = 首行之前（语义同 `1`）；`-1` = 末尾追加 |
| `--row-start` / `--row-end`（delete） | 1-based 行号，左闭右开：`--row-start 1 --row-end 3` = 删第 1、2 行 | `0` 不合法，服务端会拒绝 |
| `--cell` / `--range`（A1） | A1 记法 — 字母列 + 1-based 行号，`A1` = 第 1 列第 1 行；`A1:C3` 两端都包含 | — |
| `--col` / `--col-start` / `--col-end` | Excel 字母列，`A` = 第 1 列；`--col-start A --col-end C` 两端都包含 | `--col 0` = 首列之前插入；`--col -1` = 末尾追加（仅 `--col` 支持） |

**共用法则：**
- 正常取值 → 1-based 对齐日常表格直觉（行号从 1 开始）；
- `0` → "首"方向的哨兵（首行之前 / 首列之前）；
- `-1` → "末尾"方向的哨兵（仅 `--row-index` 和 `--col` 支持，用于 `append` 语义）。

> **实现注记（了解即可）：** CLI 层 `--row-index=0` 由于 Go 整型零值处理，会被视为"未设置"而不下发，SDK 默认按 `-1`（末尾追加）兜底。两者在空表 / 单行表上结果一致；若要显式"插到首行之前"，推荐直接传 `--row-index 1`。见 `cli/shortcuts/doc/docs_update_table.go:243-250`。

## 指令详解

### table_insert_rows — 插入行

在指定位置插入一行空行。`--row-index N`（**1-based**）= 插入后新行成为第 N 行，已有第 N 行及之后的行向下平移。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--row-index` | 是 | 1-based 行号，新行将成为第 N 行。特殊值：`0` = 首行之前（同 `1`）；`-1` = 末尾追加 |

```bash
# 末尾追加一行（最常用）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_rows \
  --table-block-id blkcnXXXX --row-index -1

# 新行成为第 2 行（原第 2 行及以后整体下移）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_rows \
  --table-block-id blkcnXXXX --row-index 2

# 首行之前插入（显式推荐写法）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_rows \
  --table-block-id blkcnXXXX --row-index 1
```

### table_insert_cols — 插入列

在指定位置插入一列空列。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--col` | 是 | 插入位置，列字母。`0` = 在第一列前插入，`-1` = 追加到末尾 |

```bash
# 在 C 列前插入一列
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_cols \
  --table-block-id blkcnXXXX --col C

# 在末尾追加一列
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_insert_cols \
  --table-block-id blkcnXXXX --col -1
```

### table_delete_rows — 删除行范围

删除连续的行。**行号 1-based**，范围左闭右开（`--row-start` 含，`--row-end` 不含）。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--row-start` | 是 | 起始行号（含），1-based，`>= 1` |
| `--row-end` | 是 | 结束行号（不含），`> row-start` |

```bash
# 删除第 2、3 行（保留第 1 行表头）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_rows \
  --table-block-id blkcnXXXX --row-start 2 --row-end 4

# 删除第 1、2 行
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_rows \
  --table-block-id blkcnXXXX --row-start 1 --row-end 3
```

### table_delete_cols — 删除列范围

删除连续的列。**注意：范围是 start-inclusive、end-exclusive**（半开区间 `[col-start, col-end)`）——`--col-end` 指定的那一列**不会**被删除。要删到某列含端，`--col-end` 填它的下一列。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--col-start` | 是 | 起始列字母（含） |
| `--col-end` | 是 | 结束列字母（**不含**）——删到 D 列含端时填 `E` |

```bash
# 删除 A 列（只 A 一列）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_cols \
  --table-block-id blkcnXXXX --col-start A --col-end B

# 删除 B、C 两列（D 保留）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_cols \
  --table-block-id blkcnXXXX --col-start B --col-end D

# 删除 B、C、D 三列（E 保留）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_cols \
  --table-block-id blkcnXXXX --col-start B --col-end E
```

### table_merge_cells — 合并单元格

合并矩形区域内的单元格。目标区域不能与已有合并区域部分重叠（必须完全包含或完全不交叉）。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--range` | 是 | A1 记法矩形区域，两端都包含 |

```bash
# 合并 A1:C2 区域（第1~2行、A~C列）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_merge_cells \
  --table-block-id blkcnXXXX --range A1:C2
```

### table_unmerge_cells — 拆分单元格

拆分已合并的单元格。指定合并区域内**任意一个**单元格坐标即可。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--cell` | 是 | 合并区域内任一单元格，A1 记法 |

```bash
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_unmerge_cells \
  --table-block-id blkcnXXXX --cell A1
```

### table_update_property — 表格/单元格属性

一条指令承载两类正交的属性：

- **表级属性**（整张表）：列宽、表头行 / 列开关
- **单元格级样式**（部分单元格）：背景色、垂直对齐 —— 支持四种互斥的定位模式

两类属性可在同一次调用里组合，但**单元格级样式的四种定位模式必须选且只选一种**。没有任何属性字段时会被拒绝（避免静默 no-op）。

#### 模式一 — 表级属性（不带任何单元格定位）

设置列宽、表头行、表头列。

| 参数 | 必填 | 说明 |
|------|------|------|
| `--col` | 视情况 | 设置 `--col-width` 时必填：目标列字母 |
| `--col-width` | 否 | 列宽（px） |
| `--header-row` | 否 | bool 开关：出现即启用首行表头；显式关闭用 `--header-row=false`；不传则不修改 |
| `--header-column` | 否 | bool 开关：出现即启用首列表头；显式关闭用 `--header-column=false`；不传则不修改 |

```bash
# 设置 B 列宽 300px + 启用表头行
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX --col B --col-width 300 --header-row

# 显式关闭表头列（注意 = 号，bool flag 不支持空格分隔值）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX --header-column=false
```

#### 模式二 — 单元格级样式（四种互斥的定位模式）

给单元格、整行、整列或 A1 矩形区域设置背景色 / 垂直对齐。**恰好选一种**定位方式，再叠加 `--background-color` 或 `--vertical-align`（至少一项）。

| 定位模式 | 参数 | 语义 |
|---------|------|------|
| **单格** | `--cell B3` | 单个单元格，A1 记法 |
| **A1 矩形** | `--range A1:C3` | 矩形区域，两端都包含 |
| **整行区间** | `--row-start N --row-end M` | 1-based 左闭右开（`--row-start 2 --row-end 5` 选中第 2、3、4 行），整行跨所有列 |
| **整列区间** | `--col-start A --col-end C` | 列字母、**两端都包含**（`--col-start A --col-end C` 选中 A、B、C 三列），整列跨所有行 |

> **列范围语义提醒：** `table_delete_cols` 的 `--col-start/--col-end` 是左闭右开（见上文），而 `table_update_property` 的 `--col-start/--col-end` 是**闭区间**，直观对齐"从 A 染到 C"。选错会多染一列或少染一列 —— 按上表确认。

| 样式参数 | 必填 | 说明 |
|---------|------|------|
| `--background-color` | 至少一个 | 命名色：`light-gray`、`light-red`、`light-orange`、`light-yellow`、`light-green`、`light-blue`、`light-purple`、`medium-gray`；或 `rgb(r,g,b)` / `rgba(r,g,b,a)` / `#RRGGBB` |
| `--vertical-align` | 至少一个 | `top` \| `middle` \| `bottom` |

```bash
# 单格：B1 浅蓝 + 垂直居中
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX \
  --cell B1 --background-color light-blue --vertical-align middle

# A1 矩形：把 A1:C3 九个单元格整块染成浅黄
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX \
  --range A1:C3 --background-color light-yellow

# 整行区间：把第 2、3、4 行全部垂直居中（典型"正文行统一样式"）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX \
  --row-start 2 --row-end 5 --vertical-align middle

# 整列区间：把 B、C 两列染成浅灰（数据列与标签列区分）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX \
  --col-start B --col-end C --background-color light-gray

# 组合：一次调用同时设表头行 + 给第 1 行染浅蓝（典型"套表头样式"）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property \
  --table-block-id blkcnXXXX \
  --header-row \
  --row-start 1 --row-end 2 --background-color light-blue
```

## 返回值

与内容操作共享相同的响应格式：

```json
{
  "ok": true,
  "data": {
    "document": { "revision_id": 15 },
    "result": "success",
    "updated_blocks_count": 1,
    "warnings": []
  }
}
```

完整 API 响应（包括 `warnings` 中的部分失败信息）会透传返回。

## 组合配方（Composition Recipes）

多步表格操作的标准顺序。严格按顺序执行可避免索引漂移和样式丢失。

### 配方 1 — 从零创建带样式表头的 5×3 表格

不走 `table_*`，而是用 `append` + XML；表头样式直接写在 XML 里最省步骤。

```bash
# 一步到位：append XML 已含表头背景色和字体
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command append \
  --content '<table>
    <thead><tr>
      <th background-color="light-blue"><b>项目</b></th>
      <th background-color="light-blue"><b>负责人</b></th>
      <th background-color="light-blue"><b>截止日期</b></th>
    </tr></thead>
    <tbody>
      <tr><td>—</td><td>—</td><td>—</td></tr>
      <tr><td>—</td><td>—</td><td>—</td></tr>
      <tr><td>—</td><td>—</td><td>—</td></tr>
      <tr><td>—</td><td>—</td><td>—</td></tr>
    </tbody>
  </table>'
```

> 见 [`lark-doc-xml.md`](lark-doc-xml.md) 的**表格画廊**章节，内含更多表头 / 合并 / 列宽 / 单元格样式示例。

### 配方 2 — 安全删除多行

**从高行号删到低行号**，避免每次删除后行号改变导致的漂移。

```bash
# 要删掉第 3、4、5 行（保留表头第 1 行和第 2 行）
# ❌ 错误做法：先删 3-4 再删 5-6 —— 原第 5 行此时已经变成第 3 行了

# ✅ 正确做法：先删末段（5-6），再删前段（3-4）
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_rows --table-block-id blkcnXXXX \
  --row-start 5 --row-end 6

lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_rows --table-block-id blkcnXXXX \
  --row-start 3 --row-end 5
```

或**一次性删掉连续区间**（更推荐）：

```bash
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_delete_rows --table-block-id blkcnXXXX \
  --row-start 3 --row-end 6   # 删第 3、4、5 行
```

插入顺序相反：**从低到高**，避免前面的插入把后面的索引推掉。

### 配方 3 — 合并单元格后设置样式和加粗文字

**顺序：merge → update-property（样式）→ block_replace（文字）**。先合并再样式，合并后目标区域只剩左上角一个单元格，样式只需作用于该单元格。

```bash
# Step 1: 合并 A1:C1 为横跨三列的标题栏
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_merge_cells --table-block-id blkcnXXXX \
  --range A1:C1

# Step 2: 给合并后的单元格加浅蓝背景、垂直居中
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command table_update_property --table-block-id blkcnXXXX \
  --cell A1 \
  --background-color light-blue --vertical-align middle

# Step 3: 把单元格文字改成加粗的"季度报表"
# 需要先从 +fetch --detail with-ids 拿到 A1 对应的 cell block ID
lark-cli docs +update --api-version v2 --doc "<url>" \
  --command block_replace --block-id <A1_cell_block_id> \
  --content '<p><b>季度报表</b></p>'
```

## 边界与约束

- **最少保留一行** — 服务端强制，删除后表格为空会被拒绝。
- **合并区域不能部分重叠** — 目标区域必须与已有合并区域完全不交叉或完全包含。合并前用 `+fetch --detail with-ids` 核对。
- **insert 与 delete 索引一致使用 1-based**（见[索引约定速查](#索引约定速查-)）——与 A1 记法对齐；`0` / `-1` 仅在 `--row-index` 和 `--col` 上作为哨兵。
- **`table_update_property` 有两类属性** — 表级（列宽 / 表头行 / 表头列）与单元格级（背景色 / 垂直对齐）可在同一次调用里组合；单元格级样式**必须且只能**选 `--cell` / `--range` / `--row-start+--row-end` / `--col-start+--col-end` 中的一种定位方式。全空调用和多定位冲突都会被立即拒绝并返回 AI 可自纠的提示。
- **布尔三态** — `--header-row`、`--header-column` 是 bool flag：不传 / `--header-row`（= true）/ `--header-row=false` 三种状态语义不同。不传 = "本次不改"；显式关闭必须用 `=` 连写（`--header-row=false`），**不支持空格分隔写法 `--header-row false`**，否则 `false` 会被当作位置参数。
- **`--row-index=0` 实现注意** — CLI 层因 Go 零值被视为"未设置"透传，SDK 默认 `-1`。如需显式"插到首行之前"，推荐 `--row-index 1`。见 `cli/shortcuts/doc/docs_update_table.go:243-250`。
- **`--revision-id` 默认 `-1`（最新）** — 需乐观并发控制时传具体版本号。

## 最佳实践

- **先 fetch 再操作** — 每次修改前用 `+fetch --detail with-ids` 确认表格现状（行数、合并区域、block ID）。
- **结构变更、样式变更、文字变更分三次下发** — 便于失败时定位问题、也便于回滚。
- **别用 `table_*` 创建新表格** — 创建走 `append` / `block_insert_after` + XML；`table_*` 只管已有表格。

## 参考

- [`lark-doc-update.md`](lark-doc-update.md) — 内容操作指令参考（str_replace / block_replace / ...）
- [`lark-doc-xml.md`](lark-doc-xml.md) — XML 语法规范（表格画廊 + 表格属性一览）
- [`../../lark-shared/SKILL.md`](../../lark-shared/SKILL.md) — 认证和全局参数

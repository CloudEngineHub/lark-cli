# Block type limits (read-only blocks)

> **前置条件：** 先阅读 [`../SKILL.md`](../SKILL.md) 了解 `docs +create` / `docs +update` 的调用方式，必要时再读 [`lark-doc-xml.md`](lark-doc-xml.md)（资源块语法的权威定义）。

## 概念区分（避免误读）

`docs +create` / `docs +update` 并非支持全部 block 类型。**这些 block 在文档里 _可以存在_（通过 fetch 读出、通过 `block_move_after` 在文档之间搬动），但 _不能创建/更新_** —— 也就是说写入路径会静默跳过它们，不会抛错也不会出现在新文档中。两条路径行为如下：

- **写入路径（`docs +create` / `docs +update --command append/overwrite/...`）**：如果输入里出现这些 block 标签，写入阶段会被静默跳过，API 返回 `code=0 / success=true`，但 block 根本没进入新文档。
- **读取路径（`docs +fetch --api-version v2`）**：原文档里**已经存在**的同类 block 仍会被序列化为对应 XML 标签输出，可读、可作为 `block_move_after` 的搬动来源。
- **跨文档移动（`docs +update --command block_move_after`）**：所有 block 都支持移动，因此这些 read-only block 可以从一篇文档搬到另一篇。`block_copy_insert_after` 也可对部分类型生效，详见 [`lark-doc-xml.md` §四 块级复制与移动](lark-doc-xml.md#四块级复制与移动)。

简单说：**这些 block 不是"完全只读"，只是"create / update 时无法新建"**。

## 已知不支持 create / update 的 block

权威定义见 [`lark-doc-xml.md` §三 资源块](lark-doc-xml.md#三资源块)（『不可创建，仅支持移动』）。下表汇总命名、典型 XML 形式与处理建议：

| 块类型 | XML 标签（snake_case）| 典型形态 | 推荐处理 |
|---|---|---|---|
| 引用同步块 | `<synced_reference>` | `<synced_reference src-token="DOC_TOKEN" src-block-id="BLK_ID"/>` | 写入路径无效，需通过 UI 手动绑定；fetch 读出后可用 `block_move_after` 搬到目标文档 |
| 源同步块 | `<synced_source>` | `<synced_source>...内容...</synced_source>` | 同上 |
| 多维表格 | `<bitable>` | `<bitable token="APP_TOKEN" table-id="TBL_ID"/>` | 在另一篇文档里嵌入已有 base 时，先 fetch 拿到 `<bitable>` 标签，再 `block_move_after` 搬过去 |
| Base 引用 | `<base_ref>` | `<base_ref ...>` | 同上 |
| OKR | `<okr>` | `<okr ...>` | 同上 |

> **复制限制（`block_copy_insert_after`）**：上述五类（外加 `task`）都**不支持复制**；img / source / whiteboard / sheet / chat_card 才支持复制。

## 定位"该块没进去"的信号

1. `docs +create` / `docs +update` 响应 `code=0`、`success=true`，UI 上却找不到预期 block
2. 随后 `docs +fetch --api-version v2` 拿回来的 XML 里**找不到本次写入的对应 tag**（写入前就存在的同类 block 仍可见，与本次写入无关）
3. round-trip diff 多出一段 `+<synced_reference ...>` / `-<synced_reference ...>` 或类似标签

出现上述任一信号时，优先怀疑 block 类型在上面表格中。

## 对 AI Agent 的影响

- **周报 / 文档模板场景**：首行 `synced_reference` 团队介绍块必须通过 UI 手动补上；skill 里应显式记录"绑定同步块"是手工步骤，不要在生成的 XML / markdown 里伪造。
- **文档迁移 / round-trip**：跨文档迁移同步块、bitable、okr 等优先用 `block_move_after`（搬不复制）；权限或环境变化导致引用断链时，需要人工修复。
- **生成式内容**：Agent 从零生成时应避免主动插入 `<synced_reference>`、`<bitable>`、`<base_ref>`、`<okr>` 等标签（写不进去）；如果是从一篇旧文档迁内容到新文档，先 fetch 这些 block 再用 `block_move_after`。

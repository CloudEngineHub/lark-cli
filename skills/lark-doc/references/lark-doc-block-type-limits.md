# Block type limits (read-only blocks)

> **前置条件：** 先阅读 [`../SKILL.md`](../SKILL.md) 了解 `docs +create` / `docs +update` 的调用方式。

`docs +create` / `docs +update` 底层的 `create-doc` MCP 工具**不支持部分 block 类型**。具体行为分为两条路径：

- **写入路径（create / update）**：markdown 中出现这些 block 时，写入阶段会被静默跳过，API 返回 `code=0 / success=true`，但 block 根本没进文档。
- **读取路径（`docs +fetch` 导出）**：同类无法稳定序列化成 markdown 的原生 block，会以占位注释 `<!-- Unsupported block type: N -->` 呈现。

两条路径都不会抛错，是 AI Agent 经常踩坑的盲区。

本文档列出已知的只读 / 受限 block 类型，供 Agent 在撰写 markdown 前自检。

## 已知只读 block（API 只支持 fetch，不支持 create / update）

| 块类型 | Markdown / HTML 形式 | 现象 | 推荐做法 |
|--------|---------------------|------|---------|
| 引用同步块 | `<reference-synced source-block-id="..." source-document-id="...">...</reference-synced>` | 静默跳过；API 返回成功但文档中不会出现此块 | 通过 UI 手动绑定；或在 skill 中把"同步块占位"作为单独的手工步骤记录 |
| 源同步块 | `<source-synced align="1">...</source-synced>` | 同上 | 同上 |

## 会产生 `<!-- Unsupported block type: N -->` 占位符的块

`docs +fetch` 导出时遇到无法序列化成 markdown 的原生 block，会以 `<!-- Unsupported block type: <N> -->` 形式占位（例如 block type 53）。这是 **`fetch-doc` 的已知限制**，典型触发者包括：

- 部分 **文档小组件（AddOns）** — `<add-ons component-type-id="..." record='{...}'/>` 子集
- **Wiki SubPageList** — `<sub-page-list wiki="..."/>` 在 wiki 节点以外的上下文
- **议程（Agenda）** 的部分子块

如果 round-trip 回灌时看到这些注释，**不要直接把注释当成 markdown 源再 create** — `create-doc` 不会把注释解析成 block。需要人工在 UI 中重建，或寻找对应的 OpenAPI 专用接口。

## 定位"该块没进去"的信号

1. `docs +create` / `docs +update` 响应 `code=0`、`success=true`，UI 上却找不到预期 block
2. 随后 `docs +fetch` 拿回来的 markdown 里该块消失，或变成 `<!-- Unsupported block type: ... -->`
3. round-trip diff 多出一段 `+<reference-synced ...>` / `-<reference-synced ...>`

出现上述任一信号时，优先怀疑 block 类型在表格中。

## 对 AI Agent 的影响

- **周报 / 文档模板场景**：首行 `reference-synced` 团队介绍块必须通过 UI 手动补上；skill 里应显式记录"绑定同步块"是手工步骤，不要用 markdown 伪造。
- **文档迁移 / round-trip**：跨文档迁移同步块会在源文档失权后出现占位 / 丢失，属于预期行为，不是 bug。
- **生成式内容**：Agent 生成 markdown 时应避免主动插入 `<reference-synced>` 等标签（生成出来也写不进去）。

## 相关 MCP 层记录

`fetch-doc` / `create-doc` 的其它接口层限制汇总见 [lark-cli-dev skill](../../lark-cli-dev/SKILL.md) 的「已知 MCP 层限制」章节，涵盖有序列表编号重置、callout emoji 误配、代码块嵌套围栏等问题。

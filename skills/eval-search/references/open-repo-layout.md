# open 仓库导航手册（Optimizer 专用）

> **读者：** `prompts/optimizer.md` 在处理 `tool_capability` 桶的 finding 时会 Read 这篇文档。
>
> **目的：** 把 `lark_as/open` 仓库当"受控沙盒" — 明确 Optimizer 允许改哪些文件、禁止碰哪些文件、改完怎么验证。

## 仓库定位

```
$GOPATH/src/code.byted.org/lark_as/open/
```

这是 lark-cli 背后的 OpenAPI 服务层（后台简称 suite.as.open）。它把飞书内部大搜 PB（MGUniversalSearch）封装成面向外部的 OAPI。CLI 调这些 OAPI，agent 调 CLI。整条链路：

```
CLI (larksuite/cli)
  → OAPI (lark_as/open)
    → kitex_gen stub (git.byted.org/ee/go/kitex_gen, 由 IDL 仓库自动生成)
      → RPC → 大搜后端
```

**Optimizer 只动 open 仓库一层。** IDL 和 kitex_gen 不动（见禁止清单）。

## 核心目录（只读懂即可）

```
biz/search_open/            ← AI Friendly 新框架，所有改动都在这里
├── entity/                 ← 每实体一个 converter 文件
│   ├── iconverter.go       ← Converter 接口定义（不动）
│   ├── chat.go             ← 参考实现（group chat 搜索）
│   ├── meeting.go          ← 参考实现（平台实体，走 SlashCommand）
│   ├── message.go / doc.go / wiki.go / user.go / mail.go / task.go / ...
│   └── timeutil.go         ← 时间格式工具（不动）
├── adapter.go              ← 调 UniversalSearch RPC（不动）
├── handler.go              ← 编排（不动）
├── pagetoken.go            ← 翻页（不动）
├── response.go             ← 错误码（不动）
├── CLAUDE.md               ← open 仓库的开发规范，读它能看懂架构
└── api_meta/{entity}/      ← 每实体 4 个 yml（search/filter/item/meta）

biz/handler/handler.go      ← 顶层路由（不动）
rpc/                        ← 旧搜索 + RPC 封装（不动）
main.go / conf/ / utils/    ← 基础设施（不动）
```

## Converter 接口速览

每个 `entity/{name}.go` 都实现同一套 5 方法接口：

```go
type Converter interface {
    EntityType() usearch.SearchEntityType
    BuildEntityItem(ctx, req) (*usearch.BaseEntity_EntityItem, error)  // OAPI Filter → PB Filter
    BuildResponseItem(result *usearch.SearchResult) (interface{}, error)  // PB Meta → OAPI Item
    BuildDisplayInfo(result *usearch.SearchResult) string  // 组装给 AI 看的 markdown 卡片
    Prune(item interface{}, fields []string) interface{}  // 字段裁剪
}
```

**AI friendly 的高杠杆改动点几乎全在 `BuildDisplayInfo`**：它返回的 markdown 就是 agent 在 CLI 里看到的搜索结果文本。大搜结果里的标题、摘要、上下文、高亮（`<h></h>` 标签）的组装方式直接决定 agent 能否一眼判断相关性。

## ✅ 允许改动（白名单）

以下三类改动 Optimizer 可以直接写 diff，不需要动 IDL：

### 1. `BuildDisplayInfo` 优化

- 补充 markdown 字段（例如加入更多上下文、路径信息、作者、时间）
- 调整高亮策略（命中词用 `<h></h>` 标签包裹）
- 修复格式化 bug（换行、空字段处理、转义）

**边界：** 只能使用 `*usearch.SearchResult` 里已有的字段。要是需要 PB 没返回的信息，那是 PB/IDL 的问题，降级为 issue。

### 2. `BuildResponseItem` 的字段映射 bug fix

- `nil` 指针防御
- 时间戳转换错误（`UnixToISO8601` / `UnixMsToISO8601` 用错）
- 枚举值映射错（比如 `chatStatusNormal` 漏判）
- ID 字段赋值缺失

**边界：** 只能在已有 OAPI 响应字段上做映射修复；**不能**新增 OAPI 响应字段（那是 IDL 级别的契约变更）。

### 3. `Prune` 敏感字段裁剪

- 根据业务需要把敏感/内部字段从响应里去掉

### 4. 配套测试

- 每次改 `entity/{name}.go` **必须**同时更新 `entity/{name}_test.go`，否则 quality gate（未来启用）会 block

## ❌ 禁止改动（硬黑名单）

| 路径 | 原因 |
|------|------|
| `../lark/idl/**` | IDL 在另一个仓库，需要跑 overpass + go get，不是 PR 范畴 |
| `biz/search_open/handler.go` | 编排逻辑，动了容易坏所有实体 |
| `biz/search_open/adapter.go` | RPC 适配层，牵扯协议 |
| `biz/search_open/pagetoken.go` | 翻页 + Redis，幂等性敏感 |
| `biz/search_open/response.go` | 错误码契约 |
| `biz/search_open/entity/iconverter.go` | Converter 接口，动了所有实体都得跟 |
| `biz/search_open/entity/timeutil.go` | 时间工具，动了影响所有实体 |
| `biz/search_open/api_meta/**/*.yml` | 新增 / 修改 schema = 契约变更，走人工 |
| `biz/handler/handler.go` | 顶层路由 |
| `rpc/**` | 旧搜索 + RPC 封装 |
| `main.go` / `conf/**` / `utils/**` | 基础设施 |
| `go.mod` / `go.sum` | 依赖升级人工做 |

**触犯任一条** → finding 必须进 `unhandled_findings.md`，附带 issue 描述建议，不写进 diff。

## 新增 OAPI 字段（即使是 optional）的处理

**Optimizer 不能自动加字段。** 流程太复杂：

1. 需要改 IDL 仓库（`$GOPATH/src/code.byted.org/lark/idl/idl/suite/as/open/*.thrift`）
2. 需要跑 overpass 生成 kitex_gen stub
3. 需要 `go get` 拉 stub 更新
4. 需要同步改 open 仓库的 converter 映射
5. 需要同步改 `api_meta/{entity}/*.yml` schema

这是多仓库协作 + 手工步骤，Optimizer 不应该做。改为产出 GitHub issue 正文，正文包含：

- 哪个 entity 需要新字段
- 字段含义（含 proto 里已有的来源字段，若有）
- driving case 的引用
- 对 agent 决策的价值说明

issue 正文写进 `unhandled_findings.md` 的 `proposed_issue` 段，由人工创建。

## 验证策略（当前版本）

**Quality gate 暂未启用**（`/eval-search propose-pr` 跳过 open 仓库测试）。原因：open 仓库跑测试需要下游依赖，CI 配置不是 harness 可控的。PR 开出去之后，open 仓库的 CI 会自己跑。

Optimizer 自己必须做的最小校验：

1. 所有改动文件 `gofmt` 过
2. 改了 `entity/{name}.go` 必须同步动 `entity/{name}_test.go`（至少加一条测试覆盖修改的分支）
3. 不允许删除已有测试

## 参考文件（Optimizer 生成改动前**必读**）

- `biz/search_open/CLAUDE.md` — 开发规范原文
- `biz/search_open/entity/chat.go` — 完整 converter 参考
- `biz/search_open/entity/chat_test.go` — 测试写法参考
- `biz/search_open/entity/meeting.go` — 平台实体 converter 参考（`BuildDisplayInfo` 写法略有不同）

## 与主 agent 的交互契约

Optimizer 处理涉及 open 仓库的 finding 时，产出放在 `pr-draft/open/` 子目录（和 cli 仓库的 `pr-draft/` 同级）：

```
tests/eval-search/runs/<run-id>/pr-draft/
├── diff.patch                    # cli 仓库改动（原本就有）
├── generalization_note.json
├── unhandled_findings.md
├── commit_message.txt
└── open/                         # 新增：open 仓库改动
    ├── diff.patch                # 应用到 $GOPATH/src/code.byted.org/lark_as/open/
    ├── commit_message.txt
    └── touched_files.txt         # 命中白名单校验的冗余证据
```

主 agent 拿到两份 diff.patch 之后，分别 checkout 两个仓库、分别 apply、分别 commit、分别 `gh pr create`，在两个 PR description 里互相 link（见 `pr-generation.md`）。

# 污染预检规则

## 动机

评测集 base 自身、v1/v2 迭代记录文档、含 expected 的参考文档，都可能被 `drive +search` 命中。Executor 一旦 fetch 到，就是"开卷考试"——分数失去意义。

v2 的教训：PM 的 dataset base 在第一次跑评测时，几乎所有 query 的搜索 top-1 都是 dataset 自己。

因此 `/eval-search run` 需要两个 lark-cli profile：
- `loader-profile`：能读评测 Base，只负责拉取 live dataset 并写入 `dataset.jsonl`
- `executor-profile`：负责盲测搜索，必须不能读评测 Base

也可以用同一个人账号做时间隔离：先在有权限时运行 `--snapshot-only` 拉本地快照；随后把该账号从评测 Base 权限里移除；最后用 `--dataset-file` 从本地快照继续。第二步运行时仍会探测 executor 是否能读 Base，能读则阻断。

## 两道防线（必须叠加）

### 防线 1：专用账号（物理隔离）

harness 启动时 MUST 先对 executor profile 做账号检查：

```bash
lark-cli --profile <blind-runner> auth status
```

从返回里读 `userOpenId`，对照 [`known-tainted-tokens.md`](known-tainted-tokens.md) 的 `excluded_user_ids` 列表：
- 命中 → **拒绝启动**，报错退出：`当前账号在 excluded_user_ids 里；harness 必须用专用测试账号运行`
- 未命中 → 继续

**新建测试账号步骤**（手工一次性）：
1. 申请独立企业飞书账号（非 PM、非 dataset owner）
2. 账号不加入评测集 base 的权限，不加入"参考流程文档"的权限
3. 在 `~/.config/lark-cli/profiles/` 下建独立 profile，`lark-cli auth login --profile eval-search`
4. 评测运行时：`lark-cli --profile eval-search ...`

setup runner 还会主动探测 executor profile 是否能读取评测 Base：

```bash
lark-cli --profile <blind-runner> base +record-list \
  --as user \
  --base-token OOoEbNWhcaFOdisXDW7c0lKtn4g \
  --table-id tblGWdc19tKFZC6K \
  --view-id vewGToSnWl \
  --limit 1
```

期望结果是权限失败。若读取成功，说明 executor 可直接搜到或打开评测集，必须阻断本轮 run。

### 防线 2：Pre-flight 扫描（兜底）

即使账号做了物理隔离，某些情况下仍可能被污染（例如：某个新建文档恰好包含了答案且权限开放）。Pre-flight 作为兜底：

**流程**：

```
for each case in dataset.jsonl:
  result = lark-cli --profile <blind-runner> drive +search --query "<query>" --page-size 20
  hit_tokens = extract all obj_token / wiki_token from result
  tainted = hit_tokens ∩ known_tainted_tokens

  write to preflight.json:
    {
      "case_id": "case_001",
      "contamination_risk": len(tainted) > 0,
      "tainted_tokens": [...],
      "top_20_tokens": [...]
    }
```

实际执行时，`known_tainted_tokens` 由持久清单 [`known-tainted-tokens.md`](known-tainted-tokens.md) 和本轮 `cloud-doc/tainted_tokens.json` 合并得到。后者用于 `/eval-search cycle` 生成的临时报告文档，避免还没进入持久 blocklist 的过程材料影响本轮 after-run。

**不阻断**，只标记。原因：有时 pre-flight 命中但 Executor 最终没 fetch，这种 case 依然有效，Judge 会打出正常 recall 分。

### known_tainted_tokens 的维护

见 [`known-tainted-tokens.md`](known-tainted-tokens.md)。三类必须纳入：
1. **评测集 base 自身**：`OOoEbNWhcaFOdisXDW7c0lKtn4g`
2. **v1/v2 迭代记录 docx**：`VdUKdAXjmo9vl8xq4FrczK6unct`（含全部评测方法论 + 具体 case 分数）
3. **人类写的"答题参考"/"流程总结"**：任何在评测过程中被主 agent 写到飞书的 note

每次新增一个"讨论评测过程"的飞书文档，记得加进标记清单（或者更简单：**不要在飞书上写这种文档**，都写成本仓库 markdown）。

## Judge 怎么用 preflight 数据

Judge 读 `preflight.json` 判断 `contamination_penalty`：

```
for each case:
  if preflight[case].contamination_risk == true:
    scan trajectory for any tool_use that fetched one of tainted_tokens
    if fetched:
      if answer directly quotes tainted doc content:
        contamination_penalty = -3
      else:
        contamination_penalty = -1
    else:
      contamination_penalty = 0
  else:
    contamination_penalty = 0
```

## 常见坑

- **wiki 链接**：`wiki://space_xxx/node_yyy` 背后的 obj_token 才是真实目标。pre-flight 扫描时必须同时记录 `wiki_token` 和 `obj_token` 两层，任一命中标记清单即 tainted
- **短链 / applink**：`applink.feishu-pre.net/...` 跳转后的最终 URL 可能是 tainted，建议 Executor 遇到短链先解析一跳再判断。这条太细，v0.1 不做强约束
- **账号隔离失效**：PM 手滑把 dataset base 对全员开放，专用账号又能看到了。定期（每次 run 前）手动检查一下 base 的权限列表

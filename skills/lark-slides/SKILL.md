---
name: lark-slides
version: 1.0.0
description: "飞书幻灯片执行层：通过 Slides XML/API 读取、创建、删除、替换幻灯片页面，处理 URL/wiki token、媒体上传、XML schema、格式校验与排障。"
metadata:
  requires:
    bins: ["lark-cli"]
  cliHelp: "lark-cli slides --help"
---

# slides execution layer

> 创建完整 PPT、设计、美化、模板、素材、正式汇报场景请使用 lark-slides-creator。本 skill 只负责 XML/API 执行层。

**CRITICAL — 开始前 MUST 先用 Read 工具读取 [`../lark-shared/SKILL.md`](../lark-shared/SKILL.md)，其中包含认证、权限处理。**

**CRITICAL — 生成或修改任何 XML 之前，MUST 先读取 [xml-schema-quick-ref.md](references/xml-schema-quick-ref.md)。不要凭记忆猜测 XML 结构。**

**CRITICAL — `references/slides_xml_schema_definition.xml` 是 Slides XML 协议的唯一权威来源；Markdown reference 只是摘要。若两者或 `lark-cli schema` 输出不一致，以 schema 和 CLI 为准。**

## Scope

Use this skill for low-level execution tasks:

- Create an empty presentation or add raw slide XML.
- Read presentation or slide XML.
- Delete slides.
- Replace or insert existing slide blocks.
- Upload local media and use returned `file_token` in XML.
- Resolve `/slides/` URL tokens and `/wiki/` tokens.
- Check XML format, schema rules, and common API errors.

Do not use this skill as the primary entry for planning, visual design, template selection, asset planning, or full-deck creation. Route those requests to `lark-slides-creator`, then return here only for XML/API execution.

## Identity

Slides are usually user-owned content. Default to explicit `--as user` for slides commands.

```bash
lark-cli auth login --domain slides
```

Use `--as bot` only when the user explicitly asks for app/bot identity or the workflow intentionally creates bot-owned resources. If access fails, first check that the command did not accidentally use the wrong identity.

## URL And Wiki Tokens

| URL | Token | Handling |
| --- | --- | --- |
| `/slides/<token>` | `xml_presentation_id` | Use the path token directly. |
| `/wiki/<token>` | `wiki_token` | Resolve first with `wiki.spaces.get_node`; use `node.obj_token` only when `node.obj_type` is `slides`. |

```bash
lark-cli wiki spaces get_node --as user --params '{"token":"wiki_token"}'
lark-cli slides xml_presentations get --as user --params '{"xml_presentation_id":"obj_token"}'
```

`+replace-slide` and `+media-upload` can parse slides/wiki URLs. Raw API commands still require the real `xml_presentation_id`.

## Shortcuts

| Shortcut | Reference | Purpose |
| --- | --- | --- |
| `slides +create` | [lark-slides-create.md](references/lark-slides-create.md) | Create a presentation; optionally add pages with `--slides`; supports local image placeholders in `+create --slides`. |
| `slides +media-upload` | [lark-slides-media-upload.md](references/lark-slides-media-upload.md) | Upload a local image to a presentation and return a `file_token`. |
| `slides +replace-slide` | [lark-slides-replace-slide.md](references/lark-slides-replace-slide.md) | Replace or insert blocks on an existing slide without changing page order. |

Prefer shortcuts when they cover the operation, especially `+replace-slide` for existing-slide edits.

## API Commands

Always inspect schema before raw API calls:

```bash
lark-cli schema slides.<resource>.<method>
lark-cli slides <resource> <method> --as user --params '{}' --data '{}'
```

Core resources:

| Resource | Method | Purpose |
| --- | --- | --- |
| `xml_presentations` | `get` | Read full presentation XML and metadata. |
| `xml_presentation.slide` | `create` | Add one slide XML page. |
| `xml_presentation.slide` | `delete` | Delete a slide; a presentation must keep at least one page. |
| `xml_presentation.slide` | `get` | Read one slide XML. |
| `xml_presentation.slide` | `replace` | Low-level block replace/insert API; prefer `+replace-slide` unless you need raw control. |

## Creation Paths

For simple XML, `+create --slides` is concise:

```bash
lark-cli slides +create --as user --title "Demo" --slides '[
  "<slide xmlns=\"http://www.larkoffice.com/sml/2.0\"><style><fill><fillColor color=\"rgb(248,250,252)\"/></fill></style><data><shape type=\"text\" topLeftX=\"80\" topLeftY=\"80\" width=\"800\" height=\"100\"><content textType=\"title\"><p>Title</p></content></shape></data></slide>"
]'
```

For complex XML, long text, many special characters, Chinese paragraphs, images, or many pages, create an empty presentation first and add slides one by one. `+create --slides` is not atomic; if a later slide fails, earlier slides may already exist. Record `xml_presentation_id` and read the deck before continuing.

```bash
lark-cli slides +create --as user --title "Demo"

lark-cli slides xml_presentation.slide create --as user \
  --params '{"xml_presentation_id":"YOUR_ID"}' \
  --data "$(jq -n --arg content '<slide xmlns="http://www.larkoffice.com/sml/2.0"><data/></slide>' '{slide:{content:$content}}')"
```

To insert before an existing page, put `before_slide_id` in `--data`, not in `--params`.

## Media Upload

Slides XML image `src` must be a Lark `file_token`; do not use external HTTP(S) URLs.

- New deck with `+create --slides`: `src="@./local.png"` is allowed and the shortcut uploads it.
- Existing deck or raw `slide.create`: run `slides +media-upload` first, then write `src="<file_token>"`.
- Existing slide edit: upload first, then use `+replace-slide` with `block_insert` or `block_replace`.

Local paths must be safe paths under the current working directory. The upload limit is 20 MB.

## XML Rules

- `<slide>` direct children are only `<style>`, `<data>`, and `<note>`.
- Text belongs inside `<content><p>...</p></content>`.
- Escape raw text before writing XML: `&` becomes `&amp;`, text `<` becomes `&lt;`, and text `>` becomes `&gt;`.
- Gradient fills require `rgba()` stops with percentages, for example `linear-gradient(135deg,rgba(15,23,42,1) 0%,rgba(56,97,140,1) 100%)`.
- For `xml_presentation.slide.replace`, `block_replace` needs the target block id and text shapes need `<content/>`; `+replace-slide` injects the required wrapper details.

## Validation

This execution skill validates at the XML/API layer. Before execution, check XML well-formedness, escaping, request body shape, and `lark-cli schema` output. Visual layout quality checks belong to creator workflows, not this execution layer.

## Troubleshooting

| Symptom | Likely Cause | Next Action |
| --- | --- | --- |
| `400` XML or wrapper error | Bad XML or wrong `--data` shape | Check escaping, tag closure, and `lark-cli schema`. |
| `403` permission denied | Wrong identity or missing scope | Confirm `--as user` vs `--as bot`; re-run auth for slides scope. |
| `404` presentation/slide not found | Wrong token or unresolved wiki URL | Resolve wiki token or re-read current presentation. |
| `1061002` media params error | Raw upload API used incorrectly | Use `slides +media-upload`; slides parent type is `slide_file`. |
| `1061004` forbidden | Current identity cannot edit target deck | Use the owner identity or share the deck with the bot/user. |
| `3350001` catch-all validation failure | XML not well-formed, bad replace wrapper, missing `<content/>`, or unescaped text | Run lint, inspect failed page XML, and prefer `+replace-slide` for block edits. |
| `3350002` stale revision | `revision_id` is newer than current | Use `-1` or re-read the presentation and retry. |
| Created deck has blank/missing pages | Shell/JSON argument truncation or escaping issue | Read back XML, then continue with two-step `slide.create`. |
| Image does not show | `src` is URL or unresolved `@path` | Upload and replace with a `file_token`. |

## References

| Reference | Purpose |
| --- | --- |
| [xml-schema-quick-ref.md](references/xml-schema-quick-ref.md) | Required XML element and attribute quick reference. |
| [xml-format-guide.md](references/xml-format-guide.md) | Detailed XML structure and examples. |
| [slides_xml_schema_definition.xml](references/slides_xml_schema_definition.xml) | Full XML schema definition. |
| [lark-slides-create.md](references/lark-slides-create.md) | `+create` shortcut. |
| [lark-slides-media-upload.md](references/lark-slides-media-upload.md) | `+media-upload` shortcut. |
| [lark-slides-replace-slide.md](references/lark-slides-replace-slide.md) | `+replace-slide` shortcut. |
| [lark-slides-edit-workflows.md](references/lark-slides-edit-workflows.md) | Existing-slide read/modify/write workflows. |
| [lark-slides-xml-presentations-get.md](references/lark-slides-xml-presentations-get.md) | Raw presentation read API. |
| [lark-slides-xml-presentation-slide-create.md](references/lark-slides-xml-presentation-slide-create.md) | Raw slide create API. |
| [lark-slides-xml-presentation-slide-delete.md](references/lark-slides-xml-presentation-slide-delete.md) | Raw slide delete API. |
| [lark-slides-xml-presentation-slide-get.md](references/lark-slides-xml-presentation-slide-get.md) | Raw slide get API. |
| [lark-slides-xml-presentation-slide-replace.md](references/lark-slides-xml-presentation-slide-replace.md) | Raw slide replace API. |
| [examples.md](references/examples.md) | CLI examples. |
| [slides_demo.xml](references/slides_demo.xml) | Example presentation XML. |

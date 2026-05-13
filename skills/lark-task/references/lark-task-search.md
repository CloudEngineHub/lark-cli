# task +search

> **Prerequisites:** Please read `../lark-shared/SKILL.md` to understand authentication, global parameters, and security rules.
>
> **⚠️ Note:** This API must be called with a user identity. **Do NOT use an app identity, otherwise the call will fail.**

Search tasks by keyword and optional filters.

## When not to use `+search`

If the user asks for a list-style task review with no real keyword, prefer the list shortcuts instead of `+search`. Do not turn generic instruction words such as "查找", "总结", "进度", "紧急程度", or date phrases such as "这个月" into `--query`.

Use:

- `task +get-related-tasks` for "我关注的", "我创建的", or "与我相关的" task lists, then apply completion / due-time filtering as needed.
- `task +get-my-tasks` for "分配给我的" or "我负责的" task lists. The assignee/负责人 filter is implicit: this shortcut only lists tasks assigned to the current login user, and supports additional `--complete`, `--due-start`, and `--due-end` filters.
- `task +search` when the request includes a task title fragment or domain keyword, for example "发布会", "客户反馈", or "周会纪要".
- For "张三负责的" / "分配给某人" lists, resolve that person to `open_id` and use `task +search --assignee <open_id>`; `task +get-my-tasks` cannot change the assignee target.

For list-style review requests, the answer should be computed from task fields rather than from search ranking. After fetching candidates, verify the owner/assignee/follower dimension, completion state, due time, and task GUID. If the user asks for "所有" or a completion-rate/priority summary, paginate or use the list shortcut's full result support before calculating counts; do not summarize only the first page.

## Recommended Commands

```bash
# Search by keyword
lark-cli task +search --query "test"

# Search incomplete tasks assigned to specific users
lark-cli task +search --assignee "ou_xxx,ou_yyy" --completed=false

# Search by due time range
lark-cli task +search --query "release" --due "-1d,+7d"

# List tasks assigned to me without a keyword
lark-cli task +get-my-tasks --complete=false --due-start "<YYYY-MM-DD>" --due-end "<YYYY-MM-DD>"

# List tasks I follow without a keyword
lark-cli task +get-related-tasks --followed-by-me --include-complete=true
```

For calendar periods such as "this month" or "last week", compute the date boundaries at runtime before running the command. The placeholders above describe the boundary shape; they are not fixed example dates.

## Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `--query <string>` | No | Search keyword. If omitted, at least one filter must be provided. |
| `--creator <ids>` | No | Creator open_ids, comma-separated. |
| `--assignee <ids>` | No | Assignee open_ids, comma-separated. |
| `--follower <ids>` | No | Follower open_ids, comma-separated. |
| `--completed=<bool>` | No | Filter by completion state. |
| `--due <range>` | No | Due time range in `start,end` form. Each side supports ISO/date/relative/ms input. |
| `--page-token <string>` | No | Page token for pagination. |
| `--page-all` | No | Automatically paginate through all pages (max 40). |
| `--page-limit <int>` | No | Max page limit (default 20). |

## Workflow

1. Build the keyword and filters from the user's request.
2. If there is no real task keyword, switch to `task +get-my-tasks` or `task +get-related-tasks` and keep query empty.
3. Execute the selected command and validate returned task GUIDs, due time, completion state, and owner/assignee/follower fields against the user's constraints.
4. For priority sorting, derive urgency from due time and overdue state; for progress summaries, compute completion ratio from the returned tasks.
5. Report the matched tasks and include the next `page_token` if more results exist.

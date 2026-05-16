# lark-cli Plugin SDK

`extension/platform` is the **in-process plugin SDK** for lark-cli.
Plugins compile into a **fork** of the lark-cli binary via a blank
import; there is no `.so` loading, no RPC, no subprocess isolation.
A plugin shares the binary's address space and lifecycle.

## 5-minute hello world

```go
// myplugin/audit.go
package myplugin

import (
    "context"
    "log"

    "github.com/larksuite/cli/extension/platform"
)

func init() {
    platform.Register(
        platform.NewPlugin("audit", "0.1.0").
            Observer(platform.After, "log-cmd", platform.All(),
                func(ctx context.Context, inv platform.Invocation) {
                    log.Printf("cmd=%s err=%v", inv.Cmd().Path(), inv.Err())
                }).
            FailOpen().
            MustBuild())
}
```

Wire into a fork:

```go
// cmd/larkx/main.go in your fork
package main

import (
    _ "github.com/me/myplugin"  // blank import → init() runs

    "github.com/larksuite/cli/cmd"
    "os"
)

func main() { os.Exit(cmd.Execute()) }
```

```sh
go build -o larkx ./cmd/larkx && ./larkx config plugins show
```

You should see `audit` in the plugin list.

## What you can hook

| Hook                       | Fires                              | Can block?                       |
| -------------------------- | ---------------------------------- | -------------------------------- |
| `Observer`                 | Before / After each command        | No (fire-and-forget audit)       |
| `Wrap`                     | Around each command's RunE         | Yes (return `*AbortError`)       |
| `On(Startup/Shutdown)`     | Process lifecycle                  | N/A                              |
| `Restrict(Rule)`           | Bootstrap-time, single per binary  | Denies whole subtrees            |

## Safety contract (read this)

- A plugin calling `Restrict()` MUST declare `FailClosed`. The Builder
  flips it automatically; the lower-level `Plugin` interface rejects
  the mismatch with `restricts_mismatch`.
- Only ONE plugin per binary can call `Restrict()`. Multi-plugin
  Restrict is a deliberate `plugin_conflict` error (single-rule
  ecosystem assumption). YAML policy at `~/.lark-cli/policy.yml` is
  shadowed by any plugin Restrict.
- The `Wrap` factory runs **once per command dispatch**, not at
  install time. Long-lived state (clients, caches, metrics counters)
  must live on the Plugin struct or in package-level variables.
- Plugins cannot suppress a `command_denied`: the framework
  physically isolates denied commands from the Wrap chain (Observers
  still fire).
- Commands missing a `risk_level` annotation are denied by default
  when a Rule is active. Set `Rule.AllowUnannotated = true` (or
  `allow_unannotated: true` in yaml) to opt out during gradual
  adoption.
- Risk annotation typos (e.g. `"wrtie"`) are always denied with
  `risk_invalid` plus a "did you mean" suggestion. `AllowUnannotated`
  does NOT bypass this — typo is a code bug, not a missing
  annotation.

## Where to go next

- [Runnable example: audit observer](./examples/audit-observer/)
- [Runnable example: read-only policy](./examples/readonly-policy/)
- [Plugin author guide](../../docs/extension/plugin-author-guide.md)
- [reason_code reference](../../docs/extension/reason-codes.md)

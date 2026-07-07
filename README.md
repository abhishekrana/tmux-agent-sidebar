# tmux-agent-sidebar

A left sidebar for tmux that shows every Claude Code agent across all
your sessions and what state each one is in — so you always know which
agents are working, which need your attention, and which are done.

```
 tmux agents            2/5 ⠼
──────────────────────────────
api-server             ← here
   ⠼ claude  working      2m
     feat/rate-limit-rollout
     ⤷ 2 subagents
   ◔ claude  permission  40s
     fix/csrf-rotation
blog
   ✓ claude  done        12m
     draft/tmux-agents-post
dotfiles
   ? claude  asking       4m
     main
scratch             no agents
──────────────────────────────
 ⚠ 2 need attention
 j/k · ⏎/click jump · q hide
```

States are driven by Claude Code hooks — no pane scraping, no fragile
regexes. The instant an agent changes state (starts working, hits a
permission prompt, asks a question, finishes) the hook stamps the state
onto its tmux pane, and the sidebar picks it up on its 1s tick.

## Requirements

- tmux ≥ 3.2
- Claude Code ≥ 2.x (hooks)
- Go ≥ 1.22 (to build; only needed once)
- git (for the branch line)

## Install (TPM)

```tmux
set -g @plugin 'abhishekrana/tmux-agent-sidebar'
```

Press `prefix + I` to install. The plugin builds its binary on first
load (requires Go), then wire up the Claude Code hooks once:

```bash
~/.tmux/plugins/tmux-agent-sidebar/bin/tmux-agent-sidebar install-hooks
```

This appends the plugin's hook entries to `~/.claude/settings.json`.
It is idempotent, preserves everything else in the file (including any
hooks you already have — Claude runs all entries for an event), writes
through symlinks (stow-safe), and uses `$HOME`-relative paths. Use
`--target <file>` to write somewhere else, e.g. a project's
`.claude/settings.local.json`.

Agents started before the hooks were installed are picked up on their
next restart.

## Use

| key            | action                                    |
| -------------- | ----------------------------------------- |
| `prefix + e`   | toggle the sidebar for the current session |
| `j`/`k`, wheel | move between agents                       |
| `Enter`, click | jump to that agent's pane                 |
| `g` / `G`      | first / last agent                        |
| `q`            | hide the sidebar                          |

While the sidebar is on, it follows you: switching windows moves the
sidebar pane into the active window (one long-lived pane, so selection
and scroll position survive).

Agent states: `working` (yellow, spinner) · `permission` (red) ·
`asking` (orange) · `done` (green until you visit the pane, then gray) ·
`idle` (gray). Each agent shows its git branch and live subagent count.

## Status line segment

Add the placeholder wherever you want a compact summary:

```tmux
set -g status-right '#{agent_sidebar_status} ... the rest ...'
```

It renders like `⚠2 ●3` (attention / working) and disappears entirely
when no agents are running.

## Options

```tmux
set -g @agent-sidebar-key 'e'                # toggle key (after prefix)
set -g @agent-sidebar-width '30'             # sidebar width in columns
set -g @agent-sidebar-theme 'solarized-light' # or 'dark'
set -g @agent-sidebar-focus 'off'            # 'on' focuses sidebar on open
```

## How it works

- `install-hooks` registers `tmux-agent-sidebar hook` for the Claude
  Code lifecycle events (SessionStart, UserPromptSubmit, PreToolUse,
  PermissionRequest, Notification, Stop, SubagentStart/Stop,
  SessionEnd).
- The hook reads the event JSON, finds its pane via `$TMUX_PANE`, and
  stamps pane-scoped user options (`@agent_state`, `@agent_since`,
  `@agent_subagents`, ...). Pane options die with the pane, so cleanup
  is automatic; a guard on the pane's current command filters zombies.
- The sidebar TUI (Go, Bubble Tea) snapshots `list-panes -a` once a
  second and renders sessions alphabetically with the current one
  marked. Jumping runs `switch-client` + `select-window` +
  `select-pane`.
- A `session-window-changed` hook moves the sidebar pane into whichever
  window becomes active (`join-pane`), with a re-entrancy guard and
  self-healing if the pane died (e.g. tmux-resurrect restores).

## License

MIT

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
- Go ≥ 1.25 (to build; only needed once)
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

| key            | action                                     |
| -------------- | ------------------------------------------ |
| `prefix + e`   | toggle the sidebar in **all** sessions     |
| `j`/`k`, wheel | move between agents                        |
| `Enter`, click | jump to that agent's pane                  |
| `g` / `G`      | first / last agent                         |
| `q`            | hide the sidebar everywhere (same as toggle) |

The toggle is global: one press opens a sidebar in every session (and
sessions created while it's on get one automatically); the next press
closes them all. While on, each session's sidebar follows you:
switching windows moves the sidebar pane into the active window (one
long-lived pane, so selection and scroll position survive).

Agent states: `working` (yellow, spinner) · `permission` (red) ·
`asking` (orange) · `done` (green until you visit the pane, then gray) ·
`idle` (gray). Each agent shows its git branch and live subagent count.

## Tip: window-tab clicks that need a second try

Unrelated to this plugin but easy to blame on it — two stock-tmux
reasons a status-line tab click gets dropped: rapid clicks chain into
`SecondClick`/`TripleClick` events (unbound by default), and terminals
eat the *press* of a click that also focuses their window, delivering
only the release. Make every click count:

```tmux
bind -n SecondClick1Status switch-client -t =
bind -n TripleClick1Status switch-client -t =
bind -n MouseUp1Status switch-client -t =
```

The sidebar itself jumps on release for the same reason.

## tmux-resurrect / continuum

Whitelist the sidebar so restores relaunch it:

```tmux
set -g @resurrect-processes '"~tmux-agent-sidebar run"'
```

The rest is automatic. Resurrect can't see the sidebar's command (it's
the pane's root process), so the plugin sets resurrect's post-save hook
to stamp the command into each save; on restore the whitelist relaunches
it and the sidebar re-registers its own options and follow hook. Without
the whitelist, the restored slot is a dead shell pane you can close.

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

## Development

```bash
make build          # build bin/tmux-agent-sidebar
make unit           # unit tests (hook state machine, installer, snapshot, selection)
make e2e            # end-to-end: real tmux servers on throwaway sockets
make test           # everything
bin/tmux-agent-sidebar mockup   # render the UI with fake data in any pane
```

The e2e suite (`e2e/`) spins up an isolated tmux server per test
(`tmux -L <private socket>`, never your live server), fakes agents with
a renamed sleep(1) so `#{pane_current_command}` matches, drives real
`hook` events, and asserts against `capture-pane` — including a real
attached client (pty) pressing Enter in the sidebar and landing in the
other session with the highlight already in place.

For a local checkout instead of TPM, add to `~/.tmux.conf`:

```tmux
run-shell ~/path/to/tmux-agent-sidebar/agent-sidebar.tmux
```

Notes for hacking:

- `hook` must never exit non-zero or block — Claude Code waits on it.
- Hooks only load from `~/.claude/settings.json` (user level) or
  `.claude/settings{,.local}.json` (project level). A user-level
  `settings.local.json` is silently ignored by Claude Code.
- `toggle.sh` receives session/pane as format-expanded arguments:
  `run-shell` does not set `$TMUX_PANE`, and a bare `display-message`
  resolves against the attached client — the wrong session when the
  binding fires elsewhere.
- tmux quirk: only trim newlines from `list-panes` output; trimming
  whitespace eats trailing empty format fields of the last line.

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

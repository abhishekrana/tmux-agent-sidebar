# CLAUDE.md

Left tmux sidebar showing every Claude Code agent across all sessions,
state driven by Claude Code hooks. Go + Bubble Tea TUI with shell glue.
README covers usage and architecture — read its "Notes for hacking"
before touching anything that talks to tmux.

## Commands

```bash
make build              # bin/tmux-agent-sidebar
make unit               # go test -short ./...
make e2e                # full lifecycle against throwaway tmux servers
make test               # everything
go test ./e2e/ -run TestName -v -count=1   # single e2e test
bin/tmux-agent-sidebar mockup              # UI preview with fake data
```

## Layout

- `cmd/tmux-agent-sidebar` — subcommands: `run`, `mockup`, `status`,
  `hook`, `install-hooks`
- `internal/hook` — event JSON → `@agent_*` pane options; `Decide()` is
  pure, `Install()` merges into Claude settings (symlink-safe)
- `internal/tmux` — exec wrapper, `list-panes -a` snapshot, branch
  cache, status segment
- `internal/ui` — Bubble Tea TUI: `app.go` (state, mouse, selection
  sync), `render.go` (blocks, highlight), `theme.go`
- `scripts/` — `toggle.sh` (global), `open.sh`, `follow.sh`,
  `resurrect-save.sh`
- `agent-sidebar.tmux` — TPM entry point

## Rules

- Never touch the live tmux server. Tests and manual checks run on
  private sockets: `tmux -L <name> -f /dev/null`.
- Detection is hooks + pane options only — never scrape pane content.
- `hook` must never exit non-zero or block; Claude Code waits on it.
- Sidebar liveness is `#{pane_current_command} == tmux-agent-sidebar`
  everywhere; never wrap the binary in a shell (breaks it).
- Mouse actions fire on release, not press.
- Comments: one short line, only for what the code can't say.
- After changing behavior, add or extend an e2e test that fails without
  the change.

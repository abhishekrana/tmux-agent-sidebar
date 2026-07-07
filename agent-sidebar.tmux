#!/usr/bin/env bash
# TPM entry point for tmux-agent-sidebar.
#
# Options (set in ~/.tmux.conf before the TPM run line):
#   @agent-sidebar-key     toggle key after prefix       (default: e)
#   @agent-sidebar-width   sidebar width in columns      (default: 30)
#   @agent-sidebar-theme   solarized-light | dark        (default: solarized-light)
#   @agent-sidebar-focus   'on' to focus the sidebar when opening
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$CURRENT_DIR/bin/tmux-agent-sidebar"

# Build on install when no binary is shipped and Go is available.
if [ ! -x "$BIN" ] && command -v go >/dev/null 2>&1; then
    (cd "$CURRENT_DIR" && go build -o bin/tmux-agent-sidebar ./cmd/tmux-agent-sidebar) ||
        tmux display-message "tmux-agent-sidebar: go build failed"
fi
if [ ! -x "$BIN" ]; then
    tmux display-message "tmux-agent-sidebar: missing binary (install Go and reload)"
    exit 0
fi

key=$(tmux show-option -gqv @agent-sidebar-key)
key=${key:-e}
# Formats expand when the binding fires, anchoring the script to the
# session/pane where the key was pressed.
tmux bind-key "$key" run-shell "$CURRENT_DIR/scripts/toggle.sh '#{session_name}' '#{pane_id}'"

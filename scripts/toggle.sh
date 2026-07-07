#!/usr/bin/env bash
# Global sidebar toggle (bound to prefix+e).
#
# If any session has a live sidebar: close them all everywhere.
# Otherwise: open one in every session, and install a global
# session-created hook so sessions born later get one too.
# State is derived from live panes, never from a stored flag, so it
# can't go stale.
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

close_in() {
    local session=$1 pane panes
    pane=$(tmux show-option -t "$session" -qv @sidebar_pane)
    panes=$(tmux list-panes -s -t "$session" -F '#{pane_id} #{pane_current_command}' 2>/dev/null) || panes=""
    if [ -n "$pane" ] && grep -q "^$pane tmux-agent-sidebar$" <<<"$panes"; then
        tmux kill-pane -t "$pane"
    fi
    tmux set-option -t "$session" -uq @sidebar_pane
    tmux set-option -t "$session" -uq @sidebar_on
    tmux set-option -t "$session" -uq @sidebar_moving
    tmux set-hook -u -t "$session" session-window-changed 2>/dev/null || true
}

any_alive() {
    local session pane panes
    while IFS= read -r session; do
        pane=$(tmux show-option -t "$session" -qv @sidebar_pane)
        [ -n "$pane" ] || continue
        panes=$(tmux list-panes -s -t "$session" -F '#{pane_id} #{pane_current_command}' 2>/dev/null) || continue
        if grep -q "^$pane tmux-agent-sidebar$" <<<"$panes"; then
            return 0
        fi
    done < <(tmux list-sessions -F '#{session_name}')
    return 1
}

if any_alive; then
    tmux set-hook -gu session-created 2>/dev/null || true
    while IFS= read -r session; do
        close_in "$session"
    done < <(tmux list-sessions -F '#{session_name}')
else
    while IFS= read -r session; do
        "$PLUGIN_DIR/scripts/open.sh" "$session"
    done < <(tmux list-sessions -F '#{session_name}')
    tmux set-hook -g session-created \
        "run-shell '$PLUGIN_DIR/scripts/open.sh #{hook_session_name}'"
fi

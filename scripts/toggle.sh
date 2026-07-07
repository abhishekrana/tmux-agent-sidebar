#!/usr/bin/env bash
# Toggle the agent sidebar for the current session (bound to prefix+e).
#
# On:  full-height pane on the left of the current window running the
#      sidebar TUI, plus session hooks that drag the pane along when the
#      active window changes (see follow.sh).
# Off: kill the pane, drop the session options and hooks.
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$PLUGIN_DIR/bin/tmux-agent-sidebar"

# Fires on any active-window change of the session, whatever command
# caused it (select-window, next-window, M-1..9 bindings, new-window...).
FOLLOW_HOOK=session-window-changed

# Session and pane arrive as arguments, expanded by tmux at fire time
# ("run-shell 'toggle.sh #{session_name} #{pane_id}'"): a bare
# display-message here would resolve against the attached client — the
# wrong session when invoked for another one, and run-shell does not
# set TMUX_PANE.
session=${1:?usage: toggle.sh <session> <pane>}
anchor=${2:?usage: toggle.sh <session> <pane>}
width=$(tmux show-option -gqv @agent-sidebar-width)
width=${width:-30}
theme=$(tmux show-option -gqv @agent-sidebar-theme)
theme=${theme:-solarized-light}

pane=$(tmux show-option -t "$session" -qv @sidebar_pane)

# grep on a captured variable, not on a pipe: grep -q closing a pipe
# early makes list-panes exit on SIGPIPE, which pipefail turns into a
# false negative.
sidebar_alive() {
    [ -n "$pane" ] || return 1
    local panes
    panes=$(tmux list-panes -s -t "$session" -F '#{pane_id} #{pane_current_command}' 2>/dev/null) || return 1
    grep -q "^$pane tmux-agent-sidebar$" <<<"$panes"
}

clear_state() {
    tmux set-option -t "$session" -uq @sidebar_pane
    tmux set-option -t "$session" -uq @sidebar_on
    tmux set-hook -u -t "$session" "$FOLLOW_HOOK" 2>/dev/null || true
}

if sidebar_alive; then
    tmux kill-pane -t "$pane"
    clear_state
    exit 0
fi

# Stale state (sidebar died, tmux-resurrect corpse, ...): reset, then open.
clear_state

active=$anchor
new=$(tmux split-window -hbf -l "$width" -t "$anchor" -P -F '#{pane_id}' "$BIN run --theme $theme")
tmux set-option -t "$session" -q @sidebar_pane "$new"
tmux set-option -t "$session" -q @sidebar_on 1

focus=$(tmux show-option -gqv @agent-sidebar-focus)
if [ "$focus" != "on" ]; then
    tmux select-pane -t "$active"
fi

tmux set-hook -t "$session" "$FOLLOW_HOOK" "run-shell '$PLUGIN_DIR/scripts/follow.sh #{session_name}'"

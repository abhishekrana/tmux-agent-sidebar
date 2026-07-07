#!/usr/bin/env bash
# Open the sidebar in one session (no-op if it already has a live one).
# Used by toggle.sh for every session and by the session-created hook
# for sessions born while the sidebar is globally on.
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$PLUGIN_DIR/bin/tmux-agent-sidebar"

session=${1:?usage: open.sh <session>}

width=$(tmux show-option -gqv @agent-sidebar-width)
width=${width:-30}
theme=$(tmux show-option -gqv @agent-sidebar-theme)
theme=${theme:-solarized-light}

panes=$(tmux list-panes -s -t "$session" -F '#{pane_id} #{pane_current_command}' 2>/dev/null) || exit 0
# Any pane already running the sidebar counts: our own from a previous
# open, or an orphan restored by tmux-resurrect (options and hooks are
# not saved, so re-stamp them and adopt the pane instead of stacking a
# second sidebar next to it).
alive=$(awk '$2 == "tmux-agent-sidebar" {print $1; exit}' <<<"$panes")
if [ -n "$alive" ]; then
    tmux set-option -t "$session" -q @sidebar_pane "$alive"
    tmux set-option -t "$session" -q @sidebar_on 1
    tmux set-hook -t "$session" session-window-changed \
        "run-shell '$PLUGIN_DIR/scripts/follow.sh #{session_name}'"
    exit 0
fi

# Active pane of the session's active window: anchor for the split and
# where focus returns afterwards.
active=$(tmux display-message -p -t "$session" '#{pane_id}')
new=$(tmux split-window -hbf -l "$width" -t "$active" -P -F '#{pane_id}' "$BIN run --theme $theme")
tmux set-option -t "$session" -q @sidebar_pane "$new"
tmux set-option -t "$session" -q @sidebar_on 1

focus=$(tmux show-option -gqv @agent-sidebar-focus)
if [ "$focus" != "on" ]; then
    tmux select-pane -t "$active"
fi

tmux set-hook -t "$session" session-window-changed \
    "run-shell '$PLUGIN_DIR/scripts/follow.sh #{session_name}'"

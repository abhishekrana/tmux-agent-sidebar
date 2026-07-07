#!/usr/bin/env bash
# tmux-resurrect post-save hook. Resurrect saves the pane shell's child
# command; for sidebar panes that's empty or the blocked `tmux wait-for`
# helper, so stamp the real command for the whitelist to relaunch.
set -euo pipefail

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_file=${1:?usage: resurrect-save.sh <state-file>}

theme=$(tmux show-option -gqv @agent-sidebar-theme)
theme=${theme:-solarized-light}
cmd="$PLUGIN_DIR/bin/tmux-agent-sidebar run --theme $theme"

sed -i -E "s|\ttmux-agent-sidebar\t:.*$|\ttmux-agent-sidebar\t:$cmd|" "$state_file"

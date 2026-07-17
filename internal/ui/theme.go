package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/model"
)

// Theme is the sidebar palette. The default matches the user's terminal
// background (no explicit bg fill) so it blends into any tmux window style.
type Theme struct {
	Fg       lipgloss.Color // default text
	Muted    lipgloss.Color // separators, hints, idle
	Emphasis lipgloss.Color // session names
	Accent   lipgloss.Color // current-session marker
	SelBg    lipgloss.Color // selected row background
	Working  lipgloss.Color
	Perm     lipgloss.Color
	Question lipgloss.Color
	Done     lipgloss.Color
}

// SolarizedLight matches the user's tmux/nvim/hunk Solarized Light stack.
func SolarizedLight() Theme {
	return Theme{
		Fg:       lipgloss.Color("#657b83"), // base00
		Muted:    lipgloss.Color("#93a1a1"), // base1
		Emphasis: lipgloss.Color("#586e75"), // base01
		Accent:   lipgloss.Color("#268bd2"), // blue
		SelBg:    lipgloss.Color("#eee8d5"), // base2
		Working:  lipgloss.Color("#2aa198"), // cyan — active but calm, not shouting
		Perm:     lipgloss.Color("#b58900"), // amber — needs you
		Question: lipgloss.Color("#b58900"), // amber — needs you
		Done:     lipgloss.Color("#859900"), // green — ready to review
	}
}

// Dark is a generic palette for dark terminals.
func Dark() Theme {
	return Theme{
		Fg:       lipgloss.Color("#c8c8c8"),
		Muted:    lipgloss.Color("#6c6c6c"),
		Emphasis: lipgloss.Color("#e4e4e4"),
		Accent:   lipgloss.Color("#5fafff"),
		SelBg:    lipgloss.Color("#303030"),
		Working:  lipgloss.Color("#2aa198"), // cyan — active but calm
		Perm:     lipgloss.Color("#d7af00"), // amber — needs you
		Question: lipgloss.Color("#d7af00"), // amber — needs you
		Done:     lipgloss.Color("#87af00"),
	}
}

// ThemeByName resolves the @agent-sidebar-theme option value.
func ThemeByName(name string) Theme {
	switch name {
	case "dark":
		return Dark()
	default:
		return SolarizedLight()
	}
}

// StateColor maps an agent state to its theme color.
func (t Theme) StateColor(s model.AgentState) lipgloss.Color {
	switch s {
	case model.StateWorking:
		return t.Working
	case model.StatePermission:
		return t.Perm
	case model.StateQuestion:
		return t.Question
	case model.StateDone:
		return t.Done
	default:
		return t.Muted
	}
}

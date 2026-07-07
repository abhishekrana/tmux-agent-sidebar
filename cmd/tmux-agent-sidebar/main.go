// tmux-agent-sidebar: a left tmux sidebar showing every Claude Code agent
// across all sessions and its state (working / needs attention / done).
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abhishekrana/tmux-agent-sidebar/internal/ui"
)

const usage = `usage: tmux-agent-sidebar <command>

commands:
  mockup [--theme light|dark]   render the sidebar with fake data (visual preview)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "mockup":
		runMockup(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func themeFlag(args []string) ui.Theme {
	for i, a := range args {
		if a == "--theme" && i+1 < len(args) {
			return ui.ThemeByName(args[i+1])
		}
	}
	return ui.SolarizedLight()
}

func runMockup(args []string) {
	app := ui.NewMockup(themeFlag(args))
	if _, err := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Miten15/NodeMop/internal/tui"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid path:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(abs))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "NodeMop failed:", err)
		os.Exit(1)
	}
}

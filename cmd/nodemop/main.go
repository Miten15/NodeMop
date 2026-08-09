package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Miten15/NodeMop/internal/dockertui"
	"github.com/Miten15/NodeMop/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "docker" {
		p := tea.NewProgram(dockertui.New())
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "NodeMop Docker Mop failed:", err)
			os.Exit(1)
		}
		return
	}

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

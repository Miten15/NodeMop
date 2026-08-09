package dockertui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Miten15/NodeMop/internal/dockerx"
)

type mode int

const (
	modeList mode = iota
	modeConfirm
	modeCleaning
)

type scanDoneMsg struct {
	summary dockerx.Summary
	err     error
}

type pruneDoneMsg struct {
	output string
	err    error
}

type row struct {
	label       string
	description string
	category    dockerx.Category
	safeDefault bool
}

type Model struct {
	mode     mode
	loading  bool
	summary  dockerx.Summary
	err      error
	cursor   int
	selected map[int]bool
	message  string
	width    int
	height   int
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F5F"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590"))
	accentStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58A6FF"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D29922"))
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3FB950"))
	badStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F85149"))
)

func rows() []row {
	return []row{
		{label: "Stopped containers", description: "Containers that are not running", category: dockerx.StoppedContainers, safeDefault: true},
		{label: "Unused images", description: "Images not referenced by any container", category: dockerx.UnusedImages, safeDefault: true},
		{label: "Build cache", description: "Unused Docker build cache", category: dockerx.BuildCache, safeDefault: true},
		{label: "Unused networks", description: "Networks not used by any container", category: dockerx.UnusedNetworks, safeDefault: true},
	}
}

func New() Model {
	selected := map[int]bool{}
	for i, item := range rows() {
		selected[i] = item.safeDefault
	}
	return Model{mode: modeList, loading: true, selected: selected}
}

func (m Model) Init() tea.Cmd { return scanCmd() }

func scanCmd() tea.Cmd {
	return func() tea.Msg {
		summary, err := dockerx.Scan()
		return scanDoneMsg{summary: summary, err: err}
	}
}

func pruneCmd(categories []dockerx.Category) tea.Cmd {
	return func() tea.Msg {
		output, err := dockerx.Prune(categories)
		return pruneDoneMsg{output: output, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case scanDoneMsg:
		m.loading = false
		m.summary = msg.summary
		m.err = msg.err
	case pruneDoneMsg:
		m.mode = modeList
		if msg.err != nil {
			m.message = "Docker cleanup failed: " + msg.err.Error()
		} else {
			m.message = "Docker cleanup complete."
		}
		m.loading = true
		return m, scanCmd()
	case tea.KeyPressMsg:
		cmd := m.handleKey(msg.String())
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(key string) tea.Cmd {
	if key == "ctrl+c" {
		return tea.Quit
	}

	switch m.mode {
	case modeCleaning:
		return nil
	case modeConfirm:
		switch key {
		case "y", "Y", "enter":
			categories := m.selectedCategories()
			if len(categories) == 0 {
				m.mode = modeList
				m.message = "Nothing selected."
				return nil
			}
			m.mode = modeCleaning
			return pruneCmd(categories)
		case "n", "N", "esc", "q":
			m.mode = modeList
		}
		return nil
	}

	switch key {
	case "q", "esc":
		return tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(rows())-1 {
			m.cursor++
		}
	case "space":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "x", "enter":
		if len(m.selectedCategories()) > 0 {
			m.mode = modeConfirm
		}
	case "r":
		m.loading = true
		m.err = nil
		m.message = ""
		return scanCmd()
	case "a":
		for i := range rows() {
			m.selected[i] = true
		}
	case "c":
		m.selected = map[int]bool{}
	}
	return nil
}

func (m Model) selectedCategories() []dockerx.Category {
	items := rows()
	var categories []dockerx.Category
	for i := range items {
		if m.selected[i] {
			categories = append(categories, items[i].category)
		}
	}
	return categories
}

func (m Model) View() tea.View {
	var content string
	switch m.mode {
	case modeConfirm:
		content = m.confirmView()
	case modeCleaning:
		content = m.workingView()
	default:
		content = m.listView()
	}
	return tea.NewView(content)
}

func header() string {
	return titleStyle.Render("NodeMop") + "  " + dimStyle.Render("Docker Mop") + "\n\n"
}

func (m Model) listView() string {
	var b strings.Builder
	b.WriteString(header())

	if m.loading {
		b.WriteString("Inspecting Docker storage…\n")
		return b.String()
	}
	if m.err != nil {
		b.WriteString(badStyle.Render("Docker scan failed: "+m.err.Error()) + "\n\n")
		b.WriteString("Press r to retry or esc to exit.\n")
		return b.String()
	}
	if !m.summary.Installed {
		b.WriteString(warnStyle.Render("Docker CLI was not found in PATH.") + "\n")
		b.WriteString("Install Docker / Docker Desktop and try again.\n\n")
		b.WriteString(dimStyle.Render("esc back") + "\n")
		return b.String()
	}
	if !m.summary.Running {
		b.WriteString(warnStyle.Render("Docker is installed, but the Docker engine is not running.") + "\n")
		b.WriteString("Start Docker Desktop or the Docker daemon, then press r.\n\n")
		b.WriteString(dimStyle.Render("r refresh   esc back") + "\n")
		return b.String()
	}

	b.WriteString(successStyle.Render("Docker engine: running") + "\n\n")
	b.WriteString(accentStyle.Render("Storage") + "\n")
	b.WriteString(fmt.Sprintf("%-18s %8s %8s %14s %18s\n", "TYPE", "TOTAL", "ACTIVE", "SIZE", "RECLAIMABLE"))
	b.WriteString(strings.Repeat("─", 72) + "\n")
	for _, name := range []string{"Images", "Containers", "Local Volumes", "Build Cache"} {
		u := dockerx.UsageByType(m.summary.Usage, name)
		b.WriteString(fmt.Sprintf("%-18s %8d %8d %14s %18s\n", u.Type, u.Total, u.Active, dash(u.Size), dash(u.Reclaimable)))
	}

	b.WriteString("\n")
	b.WriteString(accentStyle.Render("Cleanup") + "\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	for i, item := range rows() {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		check := "[ ]"
		if m.selected[i] {
			check = "[x]"
		}
		extra := ""
		if item.category == dockerx.StoppedContainers {
			extra = fmt.Sprintf(" (%d found)", len(m.summary.StoppedContainers))
		}
		if item.category == dockerx.UnusedNetworks {
			extra = fmt.Sprintf(" (%d found)", m.summary.UnusedNetworks)
		}
		b.WriteString(fmt.Sprintf("%s%s %-20s %s%s\n", cursor, check, item.label, dimStyle.Render(item.description), extra))
	}

	b.WriteString("\n")
	b.WriteString(warnStyle.Render("Volumes are protected and are never pruned by Docker Mop.") + "\n")
	if m.message != "" {
		b.WriteString(successStyle.Render(m.message) + "\n")
	}
	b.WriteString(dimStyle.Render("↑/↓ or j/k navigate  space toggle  a all  c clear  x/enter clean  r refresh  esc exit") + "\n")
	return b.String()
}

func (m Model) confirmView() string {
	var b strings.Builder
	b.WriteString(header())
	b.WriteString(warnStyle.Render("Confirm Docker cleanup") + "\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	items := rows()
	for i, item := range items {
		if m.selected[i] {
			b.WriteString("  • " + item.label + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("Running containers are not removed.\n")
	b.WriteString("Docker volumes are not removed.\n\n")
	b.WriteString(warnStyle.Render("Press y/Enter to continue, n/Esc to cancel.") + "\n")
	return b.String()
}

func (m Model) workingView() string {
	return header() + accentStyle.Render("Cleaning selected Docker resources…") + "\n\nPlease keep NodeMop open until Docker finishes.\n"
}

func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

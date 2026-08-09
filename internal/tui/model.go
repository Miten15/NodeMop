package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Miten15/NodeMop/internal/ai"
	"github.com/Miten15/NodeMop/internal/cleanup"
	"github.com/Miten15/NodeMop/internal/gitx"
	"github.com/Miten15/NodeMop/internal/project"
	"github.com/Miten15/NodeMop/internal/scanner"
)

type mode int

const (
	modeList mode = iota
	modeDetails
	modeConfirmCleanup
	modeCleaning
	modeAISettings
	modeAIInputKey
	modeAIInputModel
	modeGenerating
	modeSuggestion
)

type scanDoneMsg struct {
	projects []project.Project
	err      error
}

type cleanupDoneMsg struct {
	results []cleanup.Result
	err     error
}

type ollamaModelsMsg struct {
	models []ai.OllamaModel
	err    error
}

type gitInitDoneMsg struct{ err error }
type githubRemoteDoneMsg struct{ err error }
type lazygitDoneMsg struct{ err error }
type aiDoneMsg struct {
	suggestion ai.Suggestion
	err        error
}

type Model struct {
	root       string
	projects   []project.Project
	cursor     int
	selected   map[int]bool
	loading    bool
	err        error
	width      int
	height     int
	mode       mode
	message    string
	cleanQueue []int

	provider     ai.Provider
	ollamaModels []ai.OllamaModel
	ollamaIndex  int
	ollamaErr    error
	geminiKey    string
	geminiModel  string
	inputBuffer  string
	suggestion   ai.Suggestion
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F5F"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590"))
	activeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950")).Bold(true)
	oldStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922")).Bold(true)
	noGitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Bold(true)
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Bold(true)
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922")).Bold(true)
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950")).Bold(true)
)

func New(root string) Model {
	return Model{
		root:        root,
		loading:     true,
		selected:    map[int]bool{},
		mode:        modeList,
		provider:    ai.ProviderOllama,
		geminiKey:   os.Getenv("GEMINI_API_KEY"),
		geminiModel: "gemini-2.5-flash",
	}
}

func (m Model) Init() tea.Cmd { return scanCmd(m.root) }

func scanCmd(root string) tea.Cmd {
	return func() tea.Msg {
		projects, err := scanner.Scan(root, 30*24*time.Hour)
		return scanDoneMsg{projects: projects, err: err}
	}
}

func ollamaModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		models, err := ai.ListOllamaModels(ctx)
		return ollamaModelsMsg{models: models, err: err}
	}
}

func cleanupCmd(projects []project.Project, indexes []int) tea.Cmd {
	return func() tea.Msg {
		results := make([]cleanup.Result, 0, len(indexes))
		for _, i := range indexes {
			if i < 0 || i >= len(projects) {
				continue
			}
			result, err := cleanup.Clean(projects[i].Path)
			if err != nil {
				return cleanupDoneMsg{results: results, err: fmt.Errorf("%s: %w", projects[i].Name, err)}
			}
			results = append(results, result)
		}
		return cleanupDoneMsg{results: results}
	}
}

func gitInitCmd(path string) tea.Cmd {
	return func() tea.Msg { return gitInitDoneMsg{err: gitx.Init(path)} }
}

func githubRemoteCmd(path, repoName, description string) tea.Cmd {
	return func() tea.Msg {
		return githubRemoteDoneMsg{err: gitx.CreatePrivateGitHubRemote(path, repoName, description)}
	}
}

func generateCmd(provider ai.Provider, model, key, path string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		suggestion, err := ai.Generate(ctx, provider, model, key, path)
		return aiDoneMsg{suggestion: suggestion, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case scanDoneMsg:
		m.loading = false
		m.projects = msg.projects
		m.err = msg.err
		if m.cursor >= len(m.projects) {
			m.cursor = max(0, len(m.projects)-1)
		}
	case cleanupDoneMsg:
		m.mode = modeList
		m.cleanQueue = nil
		if msg.err != nil {
			m.message = "Cleanup stopped: " + msg.err.Error()
		} else {
			var freed int64
			var dirs int
			for _, r := range msg.results {
				freed += r.FreedBytes
				dirs += len(r.Removed)
			}
			m.message = fmt.Sprintf("Cleanup complete — removed %d generated folders and freed %s.", dirs, bytes(freed))
		}
		m.selected = map[int]bool{}
		m.loading = true
		return m, scanCmd(m.root)
	case ollamaModelsMsg:
		m.ollamaModels = msg.models
		m.ollamaErr = msg.err
		if m.ollamaIndex >= len(m.ollamaModels) {
			m.ollamaIndex = 0
		}
	case gitInitDoneMsg:
		if msg.err != nil {
			m.message = "Git init failed: " + msg.err.Error()
		} else {
			m.message = "Git initialized and safety ignores added."
		}
		m.loading = true
		return m, scanCmd(m.root)
	case githubRemoteDoneMsg:
		if msg.err != nil {
			m.message = "GitHub repo creation failed: " + msg.err.Error()
		} else {
			m.message = "Private GitHub repo attached. Open Lazygit to review, commit and push."
		}
		m.loading = true
		return m, scanCmd(m.root)
	case lazygitDoneMsg:
		if msg.err != nil {
			m.message = "Lazygit exited with error: " + msg.err.Error()
		} else {
			m.message = "Returned from Lazygit — Git state rescanned."
		}
		m.loading = true
		return m, scanCmd(m.root)
	case aiDoneMsg:
		if msg.err != nil {
			m.message = "AI summary failed: " + msg.err.Error()
			m.mode = modeDetails
		} else {
			m.suggestion = msg.suggestion
			m.mode = modeSuggestion
		}
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
	case modeAIInputKey:
		m.textInput(key, true)
		return nil
	case modeAIInputModel:
		m.textInput(key, false)
		return nil
	case modeAISettings:
		return m.aiKey(key)
	case modeSuggestion:
		if key == "esc" || key == "enter" || key == "q" {
			m.mode = modeDetails
		}
		return nil
	case modeGenerating, modeCleaning:
		return nil
	case modeDetails:
		return m.detailsKey(key)
	case modeConfirmCleanup:
		switch key {
		case "y", "Y", "enter":
			m.mode = modeCleaning
			return cleanupCmd(m.projects, m.cleanQueue)
		case "n", "N", "esc", "q":
			m.mode = modeList
			m.cleanQueue = nil
		}
		return nil
	}

	switch key {
	case "q":
		return tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.projects)-1 {
			m.cursor++
		}
	case "space":
		if len(m.projects) > 0 {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	case "enter":
		if len(m.projects) > 0 {
			m.mode = modeDetails
			m.message = ""
		}
	case "o":
		for i, p := range m.projects {
			if p.Status == project.StatusOld || p.Status == project.StatusNoGit || p.GitState == project.GitDirty || p.GitState == project.GitAhead {
				m.selected[i] = true
			}
		}
	case "x":
		m.cleanQueue = m.cleanupTargets()
		if len(m.cleanQueue) > 0 {
			m.mode = modeConfirmCleanup
		}
	case "a":
		m.mode = modeAISettings
		return ollamaModelsCmd()
	case "r":
		m.loading = true
		m.err = nil
		m.message = ""
		return scanCmd(m.root)
	case "esc":
		m.selected = map[int]bool{}
		m.message = ""
	}
	return nil
}

func (m *Model) detailsKey(key string) tea.Cmd {
	if len(m.projects) == 0 {
		m.mode = modeList
		return nil
	}
	p := m.projects[m.cursor]
	switch key {
	case "esc", "backspace", "q":
		m.mode = modeList
	case "a":
		m.mode = modeAISettings
		return ollamaModelsCmd()
	case "i":
		if p.HasGit {
			m.message = "This project already belongs to a Git repository."
		} else {
			m.message = "Initializing Git…"
			return gitInitCmd(p.Path)
		}
	case "c":
		if !p.HasGit {
			m.message = "Initialize Git first with i."
			return nil
		}
		if p.HasRemote {
			m.message = "An origin remote already exists: " + p.RemoteURL
			return nil
		}
		if !gitx.Installed("gh") {
			m.message = "GitHub CLI (gh) was not found in PATH."
			return nil
		}
		m.message = "Creating private GitHub repository…"
		return githubRemoteCmd(p.Path, gitx.ArchiveRepoName(p.Name), m.suggestion.Description)
	case "l":
		if !gitx.Installed("lazygit") {
			m.message = "Lazygit was not found in PATH."
			return nil
		}
		if !p.HasGit {
			m.message = "Initialize Git first."
			return nil
		}
		cmd := exec.Command("lazygit")
		cmd.Dir = p.Path
		return tea.ExecProcess(cmd, func(err error) tea.Msg { return lazygitDoneMsg{err: err} })
	case "g":
		m.mode = modeGenerating
		m.message = ""
		return generateCmd(m.provider, m.selectedModel(), m.geminiKey, p.Path)
	}
	return nil
}

func (m *Model) aiKey(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.mode = modeList
	case "p", "tab":
		if m.provider == ai.ProviderOllama {
			m.provider = ai.ProviderGemini
		} else {
			m.provider = ai.ProviderOllama
			return ollamaModelsCmd()
		}
	case "left", "h":
		if m.provider == ai.ProviderOllama && len(m.ollamaModels) > 0 {
			m.ollamaIndex = (m.ollamaIndex - 1 + len(m.ollamaModels)) % len(m.ollamaModels)
		}
	case "right", "l":
		if m.provider == ai.ProviderOllama && len(m.ollamaModels) > 0 {
			m.ollamaIndex = (m.ollamaIndex + 1) % len(m.ollamaModels)
		}
	case "k":
		if m.provider == ai.ProviderGemini {
			m.inputBuffer = m.geminiKey
			m.mode = modeAIInputKey
		}
	case "m":
		if m.provider == ai.ProviderGemini {
			m.inputBuffer = m.geminiModel
			m.mode = modeAIInputModel
		}
	case "r":
		return ollamaModelsCmd()
	case "enter":
		m.mode = modeList
		m.message = "AI provider configured: " + string(m.provider)
	}
	return nil
}

func (m *Model) textInput(key string, secret bool) {
	switch key {
	case "esc":
		m.mode = modeAISettings
		return
	case "enter":
		if secret {
			m.geminiKey = m.inputBuffer
		} else {
			m.geminiModel = strings.TrimSpace(m.inputBuffer)
		}
		m.inputBuffer = ""
		m.mode = modeAISettings
		return
	case "backspace":
		r := []rune(m.inputBuffer)
		if len(r) > 0 {
			m.inputBuffer = string(r[:len(r)-1])
		}
		return
	}
	if len(key) == 1 {
		m.inputBuffer += key
	}
}

func (m Model) selectedModel() string {
	if m.provider == ai.ProviderOllama {
		if len(m.ollamaModels) == 0 {
			return ""
		}
		return m.ollamaModels[m.ollamaIndex].Name
	}
	return m.geminiModel
}

func (m Model) cleanupTargets() []int {
	var indexes []int
	for i := range m.projects {
		if m.selected[i] {
			indexes = append(indexes, i)
		}
	}
	if len(indexes) == 0 && len(m.projects) > 0 {
		indexes = append(indexes, m.cursor)
	}
	return indexes
}

func (m Model) View() tea.View {
	var content string
	switch m.mode {
	case modeDetails:
		content = m.detailsView()
	case modeConfirmCleanup:
		content = m.confirmCleanupView()
	case modeCleaning:
		content = m.workingView("Cleaning generated files…")
	case modeAISettings:
		content = m.aiSettingsView()
	case modeAIInputKey:
		content = m.aiInputView(true)
	case modeAIInputModel:
		content = m.aiInputView(false)
	case modeGenerating:
		content = m.workingView("Reading project metadata and generating Git suggestions…")
	case modeSuggestion:
		content = m.suggestionView()
	default:
		content = m.listView()
	}
	return tea.NewView(content)
}

func (m Model) header() string {
	return titleStyle.Render("NodeMop") + "  " + dimStyle.Render("cross-platform developer project safety & cleanup") + "\n" +
		dimStyle.Render(m.root) + "\n\n"
}

func (m Model) listView() string {
	var b strings.Builder
	b.WriteString(m.header())
	if m.loading {
		b.WriteString("Scanning projects and Git state…\n")
		return b.String()
	}
	if m.err != nil {
		b.WriteString("Scan failed: " + m.err.Error() + "\n\nPress r to retry, q to quit.\n")
		return b.String()
	}
	if len(m.projects) == 0 {
		b.WriteString("No package.json projects found.\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("%-3s %-24s %-12s %-10s %-13s %-13s %9s %9s\n", "", "PROJECT", "FRAMEWORK", "ACTIVITY", "GIT", "LAST ACTIVITY", "SIZE", "CLEAN"))
	b.WriteString(strings.Repeat("─", 105) + "\n")

	rows := m.height - 11
	if rows < 5 {
		rows = 5
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := min(len(m.projects), start+rows)
	for i := start; i < end; i++ {
		p := m.projects[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		check := " "
		if m.selected[i] {
			check = selectedStyle.Render("✓")
		}
		b.WriteString(fmt.Sprintf("%s%s %-24s %-12s %-10s %-13s %-13s %9s %9s\n",
			cursor, check, trim(p.Name, 24), trim(p.Framework, 12), styleStatus(p.Status), styleGitDisplay(p), age(p.LastActivity), bytes(p.SizeBytes), bytes(p.CleanupBytes)))
	}

	var selected int
	var selectedCleanable int64
	var unsafe int
	for i, p := range m.projects {
		if m.selected[i] {
			selected++
			selectedCleanable += p.CleanupBytes
		}
		if !p.HasGit || !p.HasRemote || p.HasUncommitted || p.Ahead > 0 {
			unsafe++
		}
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(successStyle.Render(m.message) + "\n")
	}
	b.WriteString(fmt.Sprintf("%d projects • %d need Git attention • %d selected • %s reclaimable\n", len(m.projects), unsafe, selected, bytes(selectedCleanable)))
	b.WriteString(dimStyle.Render("↑/↓ or j/k navigate  enter inspect  space select  o unsafe/old  x clean  a AI  r rescan  q quit") + "\n")
	return b.String()
}

func (m Model) detailsView() string {
	var b strings.Builder
	b.WriteString(m.header())
	if len(m.projects) == 0 {
		return b.String()
	}
	p := m.projects[m.cursor]
	b.WriteString(accentStyle.Render("Project safety") + "\n")
	b.WriteString(strings.Repeat("─", 78) + "\n")
	b.WriteString(fmt.Sprintf("Project        %s\n", p.Name))
	b.WriteString(fmt.Sprintf("Path           %s\n", p.Path))
	b.WriteString(fmt.Sprintf("Framework      %s\n", p.Framework))
	b.WriteString(fmt.Sprintf("Activity       %s — %s\n", p.Status, age(p.LastActivity)))
	b.WriteString(fmt.Sprintf("Git state      %s\n", p.GitState))
	if p.HasGit {
		kind := "REPOSITORY"
		if p.IsWorktree {
			kind = "WORKTREE"
		}
		b.WriteString(fmt.Sprintf("Git type       %s\n", kind))
		b.WriteString(fmt.Sprintf("Git root       %s\n", dash(p.GitRoot)))
	}
	b.WriteString(fmt.Sprintf("Branch         %s\n", dash(p.Branch)))
	b.WriteString(fmt.Sprintf("Changed files  %d\n", p.ChangedFiles))
	b.WriteString(fmt.Sprintf("Ahead / behind %d / %d\n", p.Ahead, p.Behind))
	b.WriteString(fmt.Sprintf("Remote         %s\n", remoteText(p)))
	b.WriteString(fmt.Sprintf("Project size   %s\n", bytes(p.SizeBytes)))
	b.WriteString(fmt.Sprintf("Cleanable      %s\n", bytes(p.CleanupBytes)))
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(warnStyle.Render(m.message) + "\n\n")
	}
	if !p.HasGit {
		b.WriteString("i  initialize Git + add NodeMop safety ignores\n")
	} else {
		if !p.HasRemote {
			b.WriteString(fmt.Sprintf("c  create PRIVATE GitHub repo %s\n", gitx.ArchiveRepoName(p.Name)))
		}
		b.WriteString("l  open Lazygit here\n")
	}
	b.WriteString("g  generate AI project summary + Git suggestions\n")
	b.WriteString("a  AI provider settings\n\n")
	b.WriteString(dimStyle.Render("WT in the project list means this project belongs to a linked Git worktree.") + "\n")
	b.WriteString(dimStyle.Render("esc back") + "\n")
	return b.String()
}

func (m Model) aiSettingsView() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(accentStyle.Render("AI project summarizer") + "\n")
	b.WriteString(strings.Repeat("─", 78) + "\n")
	b.WriteString(fmt.Sprintf("Provider      %s   (p/tab switch)\n\n", m.provider))
	if m.provider == ai.ProviderOllama {
		b.WriteString("Ollama URL    http://127.0.0.1:11434\n")
		if m.ollamaErr != nil {
			b.WriteString(warnStyle.Render("Ollama not reachable: "+m.ollamaErr.Error()) + "\n")
		} else if len(m.ollamaModels) == 0 {
			b.WriteString("Ollama detected, but no installed models were returned.\n")
		} else {
			b.WriteString(fmt.Sprintf("Model         %s\n", m.ollamaModels[m.ollamaIndex].Name))
			b.WriteString(fmt.Sprintf("Installed     %d model(s)\n", len(m.ollamaModels)))
			b.WriteString("←/→           choose model\n")
		}
	} else {
		b.WriteString(fmt.Sprintf("API key       %s\n", masked(m.geminiKey)))
		b.WriteString(fmt.Sprintf("Model         %s\n", m.geminiModel))
		b.WriteString("k             edit API key (memory only)\n")
		b.WriteString("m             edit model\n")
	}
	b.WriteString("\nenter save/use   esc back   r refresh Ollama\n")
	return b.String()
}

func (m Model) aiInputView(secret bool) string {
	label := "Gemini model"
	value := m.inputBuffer
	if secret {
		label = "Gemini API key"
		value = masked(value)
	}
	return m.header() + accentStyle.Render(label) + "\n\n" + value + "_\n\n" + dimStyle.Render("enter save   esc cancel   backspace delete") + "\n"
}

func (m Model) suggestionView() string {
	return m.header() + successStyle.Render("AI Git helper") + "\n" + strings.Repeat("─", 78) + "\n" +
		"SUMMARY\n" + wrap(m.suggestion.Summary, 78) + "\n\n" +
		"GITHUB DESCRIPTION\n" + m.suggestion.Description + "\n\n" +
		"SUGGESTED COMMIT\n" + m.suggestion.CommitMessage + "\n\n" +
		dimStyle.Render("Review these suggestions before using them in Git/Lazygit.") + "\n\nenter/esc back\n"
}

func (m Model) confirmCleanupView() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(warnStyle.Render("Confirm generated-file cleanup") + "\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	var total int64
	for _, i := range m.cleanQueue {
		if i >= 0 && i < len(m.projects) {
			p := m.projects[i]
			total += p.CleanupBytes
			b.WriteString(fmt.Sprintf("  • %-32s %10s\n", trim(p.Name, 32), bytes(p.CleanupBytes)))
		}
	}
	b.WriteString(fmt.Sprintf("\nEstimated space freed: %s\n\n", bytes(total)))
	b.WriteString("Source files and Git metadata are not deleted.\n\n")
	b.WriteString(warnStyle.Render("Press y/Enter to clean, n/Esc to cancel.") + "\n")
	return b.String()
}

func (m Model) workingView(text string) string {
	return m.header() + accentStyle.Render(text) + "\n\nPlease keep NodeMop open until the operation finishes.\n"
}

func styleStatus(s project.Status) string {
	switch s {
	case project.StatusActive:
		return activeStyle.Render(string(s))
	case project.StatusOld:
		return oldStyle.Render(string(s))
	default:
		return noGitStyle.Render(string(s))
	}
}

func styleGitDisplay(p project.Project) string {
	label := string(p.GitState)
	if p.IsWorktree {
		label += "/WT"
	}
	switch p.GitState {
	case project.GitClean:
		return activeStyle.Render(label)
	case project.GitNone, project.GitDirty, project.GitDiverged:
		return noGitStyle.Render(label)
	default:
		return oldStyle.Render(label)
	}
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func age(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	if d < 24*time.Hour {
		return "today"
	}
	days := int(d.Hours() / 24)
	if days < 60 {
		return fmt.Sprintf("%dd ago", days)
	}
	if days < 730 {
		return fmt.Sprintf("%dmo ago", days/30)
	}
	return fmt.Sprintf("%dy ago", days/365)
}

func bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func remoteText(p project.Project) string {
	if !p.HasGit {
		return "—"
	}
	if !p.HasRemote {
		return "No origin remote"
	}
	return p.RemoteURL
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func masked(s string) string {
	if s == "" {
		return "<not set>"
	}
	if len(s) <= 6 {
		return strings.Repeat("•", len(s))
	}
	return s[:3] + strings.Repeat("•", 10) + s[len(s)-3:]
}

func wrap(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return "—"
	}
	var b strings.Builder
	line := 0
	for _, word := range words {
		if line > 0 && line+1+len(word) > width {
			b.WriteByte('\n')
			line = 0
		}
		if line > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	return b.String()
}

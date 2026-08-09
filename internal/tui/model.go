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

	"github.com/example/nodemop/internal/ai"
	"github.com/example/nodemop/internal/cleanup"
	"github.com/example/nodemop/internal/gitx"
	"github.com/example/nodemop/internal/project"
	"github.com/example/nodemop/internal/scanner"
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
	key := os.Getenv("GEMINI_API_KEY")
	return Model{
		root:        root,
		loading:     true,
		selected:    map[int]bool{},
		mode:        modeList,
		provider:    ai.ProviderOllama,
		geminiKey:   key,
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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		s, err := ai.Generate(ctx, provider, model, key, path)
		return aiDoneMsg{suggestion: s, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case scanDoneMsg:
		m.loading = false
		m.projects = msg.projects
		m.err = msg.err
		if m.cursor >= len(m.projects) {
			m.cursor = max(0, len(m.projects)-1)
		}
		return m, nil
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
		return m, nil
	case gitInitDoneMsg:
		if msg.err != nil {
			m.message = "Git init failed: " + msg.err.Error()
		} else {
			m.message = "Git initialized and safe .gitignore defaults added."
		}
		m.loading = true
		return m, scanCmd(m.root)
	case githubRemoteDoneMsg:
		if msg.err != nil {
			m.message = "GitHub repo creation failed: " + msg.err.Error()
		} else {
			m.message = "Private GitHub repo attached as origin. Open Lazygit to review, commit and push."
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
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeAIInputKey:
		return m.handleTextInput(key, true)
	case modeAIInputModel:
		return m.handleTextInput(key, false)
	case modeAISettings:
		return m.handleAISettings(key)
	case modeSuggestion:
		if key == "esc" || key == "enter" || key == "q" {
			m.mode = modeDetails
		}
		return m, nil
	case modeGenerating, modeCleaning:
		return m, nil
	case modeDetails:
		return m.handleDetails(key)
	case modeConfirmCleanup:
		switch key {
		case "y", "Y", "enter":
			m.mode = modeCleaning
			return m, cleanupCmd(m.projects, m.cleanQueue)
		case "n", "N", "esc", "q":
			m.mode = modeList
			m.cleanQueue = nil
		}
		return m, nil
	}

	switch key {
	case "q":
		return m, tea.Quit
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
	case "a":
		m.mode = modeAISettings
		return m, ollamaModelsCmd()
	case "esc":
		m.selected = map[int]bool{}
		m.message = ""
	case "x":
		indexes := m.cleanupTargets()
		if len(indexes) > 0 {
			m.cleanQueue = indexes
			m.mode = modeConfirmCleanup
		}
	case "r":
		m.loading = true
		m.err = nil
		m.message = ""
		return m, scanCmd(m.root)
	}
	return m, nil
}

func (m Model) handleDetails(key string) (tea.Model, tea.Cmd) {
	if len(m.projects) == 0 {
		m.mode = modeList
		return m, nil
	}
	p := m.projects[m.cursor]
	switch key {
	case "esc", "backspace", "q":
		m.mode = modeList
	case "a":
		m.mode = modeAISettings
		return m, ollamaModelsCmd()
	case "i":
		if !p.HasGit {
			m.message = "Initializing Git…"
			return m, gitInitCmd(p.Path)
		}
		m.message = "This project already has Git initialized."
	case "c":
		if !p.HasGit {
			m.message = "Initialize Git first with i."
			return m, nil
		}
		if p.HasRemote {
			m.message = "An origin remote already exists: " + p.RemoteURL
			return m, nil
		}
		if !gitx.Installed("gh") {
			m.message = "GitHub CLI (gh) was not found in PATH."
			return m, nil
		}
		description := m.suggestion.Description
		m.message = "Creating private GitHub repository…"
		return m, githubRemoteCmd(p.Path, gitx.ArchiveRepoName(p.Name), description)
	case "l":
		if !gitx.Installed("lazygit") {
			m.message = "Lazygit was not found in PATH. Install it, then rescan."
			return m, nil
		}
		if !p.HasGit {
			m.message = "Initialize Git first with i, then open Lazygit."
			return m, nil
		}
		cmd := exec.Command("lazygit")
		cmd.Dir = p.Path
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return lazygitDoneMsg{err: err} })
	case "g":
		model := m.selectedModel()
		m.mode = modeGenerating
		m.message = ""
		return m, generateCmd(m.provider, model, m.geminiKey, p.Path)
	}
	return m, nil
}

func (m Model) handleAISettings(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.mode = modeList
	case "p", "tab":
		if m.provider == ai.ProviderOllama {
			m.provider = ai.ProviderGemini
		} else {
			m.provider = ai.ProviderOllama
			return m, ollamaModelsCmd()
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
		return m, ollamaModelsCmd()
	case "enter":
		m.mode = modeList
		m.message = "AI provider configured: " + string(m.provider)
	}
	return m, nil
}

func (m Model) handleTextInput(key string, secret bool) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.mode = modeAISettings
		return m, nil
	case "enter":
		if secret {
			m.geminiKey = m.inputBuffer
		} else {
			m.geminiModel = strings.TrimSpace(m.inputBuffer)
		}
		m.inputBuffer = ""
		m.mode = modeAISettings
		return m, nil
	case "backspace":
		r := []rune(m.inputBuffer)
		if len(r) > 0 {
			m.inputBuffer = string(r[:len(r)-1])
		}
		return m, nil
	}
	if len(key) == 1 || (len(key) > 1 && key != "tab" && !strings.HasPrefix(key, "ctrl+") && key != "left" && key != "right" && key != "up" && key != "down") {
		m.inputBuffer += key
	}
	return m, nil
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
	return titleStyle.Render("NodeMop") + "  " + dimStyle.Render("developer project safety & cleanup") + "\n" + dimStyle.Render(m.root) + "\n\n"
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
		b.WriteString("No package.json projects found.\n\nPress r to rescan, q to quit.\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("%-3s %-24s %-12s %-10s %-11s %-13s %9s %9s\n", "", "PROJECT", "FRAMEWORK", "ACTIVITY", "GIT", "LAST ACTIVITY", "SIZE", "CLEAN"))
	b.WriteString(strings.Repeat("─", 103) + "\n")

	maxRows := m.height - 12
	if maxRows < 5 {
		maxRows = 5
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := min(len(m.projects), start+maxRows)
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
		line := fmt.Sprintf("%s%s %-24s %-12s %-10s %-11s %-13s %9s %9s",
			cursor, check, trim(p.Name, 24), trim(p.Framework, 12), styleStatus(p.Status), styleGit(p.GitState), age(p.LastActivity), bytes(p.SizeBytes), bytes(p.CleanupBytes))
		b.WriteString(line + "\n")
	}

	selected := 0
	var selectedCleanable int64
	needsSafety := 0
	for i, p := range m.projects {
		if m.selected[i] {
			selected++
			selectedCleanable += p.CleanupBytes
		}
		if !p.HasGit || !p.HasRemote || p.HasUncommitted || p.Ahead > 0 {
			needsSafety++
		}
	}
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(successStyle.Render(m.message) + "\n")
	}
	b.WriteString(fmt.Sprintf("%d projects • %d need Git attention • %d selected • %s reclaimable\n", len(m.projects), needsSafety, selected, bytes(selectedCleanable)))
	b.WriteString(dimStyle.Render("↑/↓ navigate  enter inspect  space select  o select unsafe/old  x clean  a AI setup  r rescan  q quit"))
	b.WriteString("\n")
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
			b.WriteString(fmt.Sprintf("c  create PRIVATE GitHub repo %s (no automatic push)\n", gitx.ArchiveRepoName(p.Name)))
		}
		b.WriteString("l  open Lazygit here (review / commit / push / pull interactively)\n")
	}
	b.WriteString("g  generate project summary + repo description + commit suggestion\n")
	b.WriteString("a  AI provider settings\n")
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("NodeMop never sends .env files or arbitrary source bodies to the AI summarizer."))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("esc back"))
	b.WriteString("\n")
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
			b.WriteString("Start Ollama and press r to retry.\n")
		} else if len(m.ollamaModels) == 0 {
			b.WriteString("Looking for locally installed models…\n")
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
		if os.Getenv("GEMINI_API_KEY") != "" {
			b.WriteString(dimStyle.Render("GEMINI_API_KEY environment variable detected."))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString("enter save/use   esc back   r refresh Ollama\n")
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("For Gemini, the key is kept only in this NodeMop process; it is not written into the project."))
	b.WriteString("\n")
	return b.String()
}

func (m Model) aiInputView(secret bool) string {
	var b strings.Builder
	b.WriteString(m.header())
	if secret {
		b.WriteString(accentStyle.Render("Gemini API key") + "\n\n")
		b.WriteString(masked(m.inputBuffer) + "_\n\n")
	} else {
		b.WriteString(accentStyle.Render("Gemini model") + "\n\n")
		b.WriteString(m.inputBuffer + "_\n\n")
	}
	b.WriteString(dimStyle.Render("enter save   esc cancel   backspace delete"))
	b.WriteString("\n")
	return b.String()
}

func (m Model) suggestionView() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(successStyle.Render("AI Git helper") + "\n")
	b.WriteString(strings.Repeat("─", 78) + "\n")
	b.WriteString("SUMMARY\n")
	b.WriteString(wrap(m.suggestion.Summary, 78) + "\n\n")
	b.WriteString("GITHUB DESCRIPTION\n")
	b.WriteString(m.suggestion.Description + "\n\n")
	b.WriteString("SUGGESTED COMMIT\n")
	b.WriteString(m.suggestion.CommitMessage + "\n\n")
	b.WriteString(dimStyle.Render("Review these suggestions yourself before using them in Git/Lazygit."))
	b.WriteString("\n\nenter/esc back\n")
	return b.String()
}

func (m Model) confirmCleanupView() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(warnStyle.Render("Confirm generated-file cleanup") + "\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	var reclaim int64
	for _, i := range m.cleanQueue {
		if i >= 0 && i < len(m.projects) {
			p := m.projects[i]
			reclaim += p.CleanupBytes
			b.WriteString(fmt.Sprintf("  • %-32s %10s\n", trim(p.Name, 32), bytes(p.CleanupBytes)))
		}
	}
	b.WriteString(fmt.Sprintf("\nEstimated space freed: %s\n\n", bytes(reclaim)))
	b.WriteString("Removes generated folders only: node_modules, .next, dist, build, coverage, .cache, .turbo.\n")
	b.WriteString("Source files and .git are not deleted.\n\n")
	b.WriteString(warnStyle.Render("Press y or Enter to clean, n/Esc to cancel."))
	b.WriteString("\n")
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

func styleGit(s project.GitState) string {
	switch s {
	case project.GitClean:
		return activeStyle.Render(string(s))
	case project.GitNone, project.GitDirty, project.GitDiverged:
		return noGitStyle.Render(string(s))
	default:
		return oldStyle.Render(string(s))
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
	var out strings.Builder
	line := 0
	for _, word := range words {
		if line > 0 && line+1+len(word) > width {
			out.WriteByte('\n')
			line = 0
		}
		if line > 0 {
			out.WriteByte(' ')
			line++
		}
		out.WriteString(word)
		line += len(word)
	}
	return out.String()
}

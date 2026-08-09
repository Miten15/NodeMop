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

type scanDoneMsg struct { projects []project.Project; err error }
type cleanupDoneMsg struct { results []cleanup.Result; err error }
type ollamaModelsMsg struct { models []ai.OllamaModel; err error }
type gitInitDoneMsg struct{ err error }
type githubRemoteDoneMsg struct{ err error }
type lazygitDoneMsg struct{ err error }
type aiDoneMsg struct { suggestion ai.Suggestion; err error }

type Model struct {
	root string
	projects []project.Project
	cursor int
	selected map[int]bool
	loading bool
	err error
	width, height int
	mode mode
	message string
	cleanQueue []int
	provider ai.Provider
	ollamaModels []ai.OllamaModel
	ollamaIndex int
	ollamaErr error
	geminiKey string
	geminiModel string
	inputBuffer string
	suggestion ai.Suggestion
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F5F"))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D8590"))
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950")).Bold(true)
	oldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922")).Bold(true)
	noGitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF")).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950")).Bold(true)
)

func New(root string) Model {
	return Model{root: root, loading: true, selected: map[int]bool{}, provider: ai.ProviderOllama, geminiKey: os.Getenv("GEMINI_API_KEY"), geminiModel: "gemini-2.5-flash"}
}
func (m Model) Init() tea.Cmd { return scanCmd(m.root) }

func scanCmd(root string) tea.Cmd { return func() tea.Msg { p, e := scanner.Scan(root, 30*24*time.Hour); return scanDoneMsg{p, e} } }
func ollamaModelsCmd() tea.Cmd { return func() tea.Msg { ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel(); models,err:=ai.ListOllamaModels(ctx); return ollamaModelsMsg{models,err} } }
func gitInitCmd(path string) tea.Cmd { return func() tea.Msg { return gitInitDoneMsg{gitx.Init(path)} } }
func githubRemoteCmd(path,name,desc string) tea.Cmd { return func() tea.Msg { return githubRemoteDoneMsg{gitx.CreatePrivateGitHubRemote(path,name,desc)} } }
func generateCmd(provider ai.Provider, model,key,path string) tea.Cmd { return func() tea.Msg { ctx,cancel:=context.WithTimeout(context.Background(),5*time.Minute); defer cancel(); s,err:=ai.Generate(ctx,provider,model,key,path); return aiDoneMsg{s,err} } }
func cleanupCmd(projects []project.Project, indexes []int) tea.Cmd { return func() tea.Msg { var out []cleanup.Result; for _,i:=range indexes { if i<0||i>=len(projects){continue}; r,e:=cleanup.Clean(projects[i].Path); if e!=nil{return cleanupDoneMsg{out,fmt.Errorf("%s: %w",projects[i].Name,e)}}; out=append(out,r) }; return cleanupDoneMsg{out,nil} } }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v:=msg.(type) {
	case tea.WindowSizeMsg: m.width,m.height=v.Width,v.Height
	case scanDoneMsg: m.loading=false; m.projects=v.projects; m.err=v.err; if m.cursor>=len(m.projects){m.cursor=max(0,len(m.projects)-1)}
	case cleanupDoneMsg:
		m.mode=modeList; m.cleanQueue=nil; var freed int64; var dirs int
		if v.err!=nil { m.message="Cleanup stopped: "+v.err.Error() } else { for _,r:=range v.results { freed+=r.FreedBytes; dirs+=len(r.Removed) }; m.message=fmt.Sprintf("Cleanup complete — removed %d generated folders and freed %s.",dirs,bytes(freed)) }
		m.selected=map[int]bool{}; m.loading=true; return m,scanCmd(m.root)
	case ollamaModelsMsg: m.ollamaModels=v.models; m.ollamaErr=v.err; if m.ollamaIndex>=len(m.ollamaModels){m.ollamaIndex=0}
	case gitInitDoneMsg: if v.err!=nil{m.message="Git init failed: "+v.err.Error()}else{m.message="Git initialized and safety ignores added."}; m.loading=true; return m,scanCmd(m.root)
	case githubRemoteDoneMsg: if v.err!=nil{m.message="GitHub repo creation failed: "+v.err.Error()}else{m.message="Private GitHub repo attached. Open Lazygit to review, commit and push."}; m.loading=true; return m,scanCmd(m.root)
	case lazygitDoneMsg: if v.err!=nil{m.message="Lazygit exited with error: "+v.err.Error()}else{m.message="Returned from Lazygit — Git state rescanned."}; m.loading=true; return m,scanCmd(m.root)
	case aiDoneMsg: if v.err!=nil{m.message="AI summary failed: "+v.err.Error();m.mode=modeDetails}else{m.suggestion=v.suggestion;m.mode=modeSuggestion}
	case tea.KeyPressMsg: return m,m.handleKey(v.String())
	}
	return m,nil
}

func (m Model) handleKey(k string) tea.Cmd {
	if k=="ctrl+c" { return tea.Quit }
	if m.mode==modeCleaning||m.mode==modeGenerating { return nil }
	if m.mode==modeAIInputKey { m.textInput(k,true); return nil }
	if m.mode==modeAIInputModel { m.textInput(k,false); return nil }
	if m.mode==modeAISettings { return m.aiKey(k) }
	if m.mode==modeSuggestion { if k=="esc"||k=="enter"||k=="q"{m.mode=modeDetails}; return nil }
	if m.mode==modeConfirmCleanup { if k=="y"||k=="Y"||k=="enter"{m.mode=modeCleaning;return cleanupCmd(m.projects,m.cleanQueue)}; if k=="n"||k=="N"||k=="esc"||k=="q"{m.mode=modeList;m.cleanQueue=nil}; return nil }
	if m.mode==modeDetails { return m.detailsKey(k) }
	switch k {
	case "q": return tea.Quit
	case "up","k": if m.cursor>0{m.cursor--}
	case "down","j": if m.cursor<len(m.projects)-1{m.cursor++}
	case "space": if len(m.projects)>0{m.selected[m.cursor]=!m.selected[m.cursor]}
	case "enter": if len(m.projects)>0{m.mode=modeDetails;m.message=""}
	case "o": for i,p:=range m.projects { if p.Status==project.StatusOld||p.Status==project.StatusNoGit||p.GitState==project.GitDirty||p.GitState==project.GitAhead{m.selected[i]=true} }
	case "x": m.cleanQueue=m.cleanupTargets(); if len(m.cleanQueue)>0{m.mode=modeConfirmCleanup}
	case "a": m.mode=modeAISettings; return ollamaModelsCmd()
	case "r": m.loading=true;m.err=nil;m.message="";return scanCmd(m.root)
	case "esc": m.selected=map[int]bool{};m.message=""
	}
	return nil
}

func (m Model) detailsKey(k string) tea.Cmd {
	if len(m.projects)==0 { m.mode=modeList; return nil }
	p:=m.projects[m.cursor]
	switch k {
	case "esc","backspace","q": m.mode=modeList
	case "a": m.mode=modeAISettings; return ollamaModelsCmd()
	case "i": if p.HasGit{m.message="This project already belongs to a Git repository."}else{m.message="Initializing Git…";return gitInitCmd(p.Path)}
	case "c":
		if !p.HasGit{m.message="Initialize Git first with i.";return nil}; if p.HasRemote{m.message="An origin remote already exists: "+p.RemoteURL;return nil}; if !gitx.Installed("gh"){m.message="GitHub CLI (gh) was not found in PATH.";return nil}; m.message="Creating private GitHub repository…"; return githubRemoteCmd(p.Path,gitx.ArchiveRepoName(p.Name),m.suggestion.Description)
	case "l": if !gitx.Installed("lazygit"){m.message="Lazygit was not found in PATH.";return nil}; if !p.HasGit{m.message="Initialize Git first.";return nil}; cmd:=exec.Command("lazygit"); cmd.Dir=p.Path; return tea.ExecProcess(cmd,func(err error)tea.Msg{return lazygitDoneMsg{err}})
	case "g": m.mode=modeGenerating;m.message="";return generateCmd(m.provider,m.selectedModel(),m.geminiKey,p.Path)
	}
	return nil
}

func (m Model) aiKey(k string) tea.Cmd {
	switch k {
	case "esc","q": m.mode=modeList
	case "p","tab": if m.provider==ai.ProviderOllama{m.provider=ai.ProviderGemini}else{m.provider=ai.ProviderOllama;return ollamaModelsCmd()}
	case "left","h": if m.provider==ai.ProviderOllama&&len(m.ollamaModels)>0{m.ollamaIndex=(m.ollamaIndex-1+len(m.ollamaModels))%len(m.ollamaModels)}
	case "right","l": if m.provider==ai.ProviderOllama&&len(m.ollamaModels)>0{m.ollamaIndex=(m.ollamaIndex+1)%len(m.ollamaModels)}
	case "k": if m.provider==ai.ProviderGemini{m.inputBuffer=m.geminiKey;m.mode=modeAIInputKey}
	case "m": if m.provider==ai.ProviderGemini{m.inputBuffer=m.geminiModel;m.mode=modeAIInputModel}
	case "r": return ollamaModelsCmd()
	case "enter": m.mode=modeList;m.message="AI provider configured: "+string(m.provider)
	}
	return nil
}

func (m Model) textInput(k string, secret bool) {
	switch k { case "esc":m.mode=modeAISettings;return; case "enter":if secret{m.geminiKey=m.inputBuffer}else{m.geminiModel=strings.TrimSpace(m.inputBuffer)};m.inputBuffer="";m.mode=modeAISettings;return; case "backspace":r:=[]rune(m.inputBuffer);if len(r)>0{m.inputBuffer=string(r[:len(r)-1])};return }
	if len(k)==1 { m.inputBuffer+=k }
}

func (m Model) selectedModel() string { if m.provider==ai.ProviderOllama{if len(m.ollamaModels)==0{return ""};return m.ollamaModels[m.ollamaIndex].Name};return m.geminiModel }
func (m Model) cleanupTargets() []int { var x []int; for i:=range m.projects{if m.selected[i]{x=append(x,i)}};if len(x)==0&&len(m.projects)>0{x=append(x,m.cursor)};return x }

func (m Model) View() tea.View {
	var s string
	switch m.mode { case modeDetails:s=m.detailsView();case modeConfirmCleanup:s=m.confirmCleanupView();case modeCleaning:s=m.workingView("Cleaning generated files…");case modeAISettings:s=m.aiSettingsView();case modeAIInputKey:s=m.aiInputView(true);case modeAIInputModel:s=m.aiInputView(false);case modeGenerating:s=m.workingView("Reading project metadata and generating Git suggestions…");case modeSuggestion:s=m.suggestionView();default:s=m.listView() }
	return tea.NewView(s)
}

func mascotBanner() string { return strings.TrimSpace(`
  (\_/)
  (o.o)    __
  /|_|\___/  \
   / \     ==\=
`) }
func (m Model) header() string { return titleStyle.Render("NodeMop")+"  "+dimStyle.Render("cross-platform developer project safety & cleanup")+"\n"+dimStyle.Render(mascotBanner())+"\n"+dimStyle.Render(m.root)+"\n\n" }

func (m Model) listView() string {
	var b strings.Builder;b.WriteString(m.header())
	if m.loading{b.WriteString("Scanning projects and Git state…\n");return b.String()};if m.err!=nil{b.WriteString("Scan failed: "+m.err.Error()+"\n\nPress r to retry, q to quit.\n");return b.String()};if len(m.projects)==0{b.WriteString("No package.json projects found.\n");return b.String()}
	b.WriteString(fmt.Sprintf("%-3s %-24s %-12s %-10s %-13s %-13s %9s %9s\n","","PROJECT","FRAMEWORK","ACTIVITY","GIT","LAST ACTIVITY","SIZE","CLEAN"));b.WriteString(strings.Repeat("─",105)+"\n")
	rows:=m.height-16;if rows<5{rows=5};start:=0;if m.cursor>=rows{start=m.cursor-rows+1};end:=min(len(m.projects),start+rows)
	for i:=start;i<end;i++{p:=m.projects[i];cur:="  ";if i==m.cursor{cur="> "};check:=" ";if m.selected[i]{check=selectedStyle.Render("✓")};b.WriteString(fmt.Sprintf("%s%s %-24s %-12s %-10s %-13s %-13s %9s %9s\n",cur,check,trim(p.Name,24),trim(p.Framework,12),styleStatus(p.Status),styleGitDisplay(p),age(p.LastActivity),bytes(p.SizeBytes),bytes(p.CleanupBytes)))}
	var sel int;var clean int64;var unsafe int;for i,p:=range m.projects{if m.selected[i]{sel++;clean+=p.CleanupBytes};if !p.HasGit||!p.HasRemote||p.HasUncommitted||p.Ahead>0{unsafe++}}
	b.WriteString("\n");if m.message!=""{b.WriteString(successStyle.Render(m.message)+"\n")};b.WriteString(fmt.Sprintf("%d projects • %d need Git attention • %d selected • %s reclaimable\n",len(m.projects),unsafe,sel,bytes(clean)));b.WriteString(dimStyle.Render("↑/↓ navigate  enter inspect  space select  o unsafe/old  x clean  a AI  r rescan  q quit")+"\n");return b.String()
}

func (m Model) detailsView() string {
	var b strings.Builder;b.WriteString(m.header());if len(m.projects)==0{return b.String()};p:=m.projects[m.cursor];b.WriteString(accentStyle.Render("Project safety")+"\n"+strings.Repeat("─",78)+"\n");b.WriteString(fmt.Sprintf("Project        %s\nPath           %s\nFramework      %s\nActivity       %s — %s\nGit state      %s\n",p.Name,p.Path,p.Framework,p.Status,age(p.LastActivity),p.GitState))
	if p.HasGit{kind:="REPOSITORY";if p.IsWorktree{kind="WORKTREE"};b.WriteString(fmt.Sprintf("Git type       %s\nGit root       %s\n",kind,dash(p.GitRoot)))}
	b.WriteString(fmt.Sprintf("Branch         %s\nChanged files  %d\nAhead / behind %d / %d\nRemote         %s\nProject size   %s\nCleanable      %s\n\n",dash(p.Branch),p.ChangedFiles,p.Ahead,p.Behind,remoteText(p),bytes(p.SizeBytes),bytes(p.CleanupBytes)))
	if m.message!=""{b.WriteString(warnStyle.Render(m.message)+"\n\n")};if !p.HasGit{b.WriteString("i  initialize Git + add NodeMop safety ignores\n")}else{if !p.HasRemote{b.WriteString(fmt.Sprintf("c  create PRIVATE GitHub repo %s\n",gitx.ArchiveRepoName(p.Name)))};b.WriteString("l  open Lazygit here\n")};b.WriteString("g  generate AI project summary + Git suggestions\na  AI provider settings\n\n"+dimStyle.Render("WT in the project list means this project belongs to a linked Git worktree.")+"\n"+dimStyle.Render("esc back")+"\n");return b.String()
}

func (m Model) aiSettingsView() string { var b strings.Builder;b.WriteString(m.header()+accentStyle.Render("AI project summarizer")+"\n"+strings.Repeat("─",78)+"\n");b.WriteString(fmt.Sprintf("Provider      %s   (p/tab switch)\n\n",m.provider));if m.provider==ai.ProviderOllama{b.WriteString("Ollama URL    http://127.0.0.1:11434\n");if m.ollamaErr!=nil{b.WriteString(warnStyle.Render("Ollama not reachable: "+m.ollamaErr.Error())+"\n")}else if len(m.ollamaModels)==0{b.WriteString("Ollama detected, but no installed models were returned.\n")}else{b.WriteString(fmt.Sprintf("Model         %s\nInstalled     %d model(s)\n←/→           choose model\n",m.ollamaModels[m.ollamaIndex].Name,len(m.ollamaModels)))}}else{b.WriteString(fmt.Sprintf("API key       %s\nModel         %s\nk             edit API key (memory only)\nm             edit model\n",masked(m.geminiKey),m.geminiModel))};b.WriteString("\nenter save/use   esc back   r refresh Ollama\n");return b.String() }
func (m Model) aiInputView(secret bool) string { label:="Gemini model";val:=m.inputBuffer;if secret{label="Gemini API key";val=masked(val)};return m.header()+accentStyle.Render(label)+"\n\n"+val+"_\n\n"+dimStyle.Render("enter save   esc cancel   backspace delete")+"\n" }
func (m Model) suggestionView() string { return m.header()+successStyle.Render("AI Git helper")+"\n"+strings.Repeat("─",78)+"\nSUMMARY\n"+wrap(m.suggestion.Summary,78)+"\n\nGITHUB DESCRIPTION\n"+m.suggestion.Description+"\n\nSUGGESTED COMMIT\n"+m.suggestion.CommitMessage+"\n\n"+dimStyle.Render("Review these suggestions before using them in Git/Lazygit.")+"\n\nenter/esc back\n" }
func (m Model) confirmCleanupView() string { var b strings.Builder;b.WriteString(m.header()+warnStyle.Render("Confirm generated-file cleanup")+"\n"+strings.Repeat("─",72)+"\n");var total int64;for _,i:=range m.cleanQueue{if i>=0&&i<len(m.projects){p:=m.projects[i];total+=p.CleanupBytes;b.WriteString(fmt.Sprintf("  • %-32s %10s\n",trim(p.Name,32),bytes(p.CleanupBytes)))}};b.WriteString(fmt.Sprintf("\nEstimated space freed: %s\n\nSource files and Git metadata are not deleted.\n\n",bytes(total))+warnStyle.Render("Press y/Enter to clean, n/Esc to cancel.")+"\n");return b.String() }
func (m Model) workingView(s string) string { return m.header()+accentStyle.Render(s)+"\n\nPlease keep NodeMop open until the operation finishes.\n" }

func styleStatus(s project.Status) string { if s==project.StatusActive{return activeStyle.Render(string(s))};if s==project.StatusOld{return oldStyle.Render(string(s))};return noGitStyle.Render(string(s)) }
func styleGitDisplay(p project.Project) string { label:=string(p.GitState);if p.IsWorktree{label+="/WT"};switch p.GitState{case project.GitClean:return activeStyle.Render(label);case project.GitNone,project.GitDirty,project.GitDiverged:return noGitStyle.Render(label);default:return oldStyle.Render(label)} }
func trim(s string,n int)string{r:=[]rune(s);if len(r)<=n{return s};if n<=1{return string(r[:n])};return string(r[:n-1])+"…"}
func age(t time.Time)string{if t.IsZero(){return "unknown"};d:=time.Since(t);if d<24*time.Hour{return "today"};days:=int(d.Hours()/24);if days<60{return fmt.Sprintf("%dd ago",days)};if days<730{return fmt.Sprintf("%dmo ago",days/30)};return fmt.Sprintf("%dy ago",days/365)}
func bytes(n int64)string{const u=1024;if n<u{return fmt.Sprintf("%d B",n)};div,exp:=int64(u),0;for x:=n/u;x>=u;x/=u{div*=u;exp++};return fmt.Sprintf("%.1f %ciB",float64(n)/float64(div),"KMGTPE"[exp])}
func remoteText(p project.Project)string{if !p.HasGit{return "—"};if !p.HasRemote{return "No origin remote"};return p.RemoteURL}
func dash(s string)string{if s==""{return "—"};return s}
func masked(s string)string{if s==""{return "<not set>"};if len(s)<=6{return strings.Repeat("•",len(s))};return s[:3]+strings.Repeat("•",10)+s[len(s)-3:]}
func wrap(s string,width int)string{words:=strings.Fields(s);if len(words)==0{return "—"};var b strings.Builder;line:=0;for _,w:=range words{if line>0&&line+1+len(w)>width{b.WriteByte('\n');line=0};if line>0{b.WriteByte(' ');line++};b.WriteString(w);line+=len(w)};return b.String()}

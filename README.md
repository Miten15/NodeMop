# NodeMop

NodeMop is a Windows-first TUI for making old JavaScript/Node projects safe in Git, reclaiming generated-file storage, and preparing stale projects for archival.

## v0.3 — Git Safety + AI Helper

v0.3 turns NodeMop from a disk cleaner into a developer-project safety tool.

### Project scan

- Recursive discovery via `package.json`
- Detects Next.js, React, React/Vite, Vue, Svelte, Remix, Vite and generic Node.js projects
- Detects Git repositories and `origin` remotes
- Detects uncommitted files
- Detects ahead/behind status when an upstream branch exists
- Shows `CLEAN`, `DIRTY`, `NO GIT`, `NO REMOTE`, `AHEAD`, `BEHIND`, or `DIVERGED`
- Shows last activity, total size and reclaimable generated-file size

### Safe Git workflow

From a project's detail view:

- `i` initializes Git for projects without `.git`
- NodeMop appends safety-oriented `.gitignore` entries before the first commit
- `.env`, `.env.*`, `node_modules`, `.next`, build/cache output and logs are ignored by default
- `c` creates a **private** GitHub repository named `bg-<project-name>` using GitHub CLI
- NodeMop deliberately does **not** auto-push when creating the remote
- `l` opens Lazygit inside the project so you can review files, commit and push yourself
- returning from Lazygit automatically rescans Git state

This keeps the dangerous step visible: NodeMop prepares; Lazygit lets you decide exactly what is committed and pushed.

### AI project helper

Press `a` for AI settings.

#### Ollama

NodeMop checks:

```text
http://127.0.0.1:11434/api/tags
```

and lets you cycle through installed local models. No API key is needed.

#### Gemini

- Press `k` to enter a Gemini API key
- Press `m` to edit the Gemini model
- Default model: `gemini-2.5-flash`
- `GEMINI_API_KEY` is automatically detected if already set
- Keys entered in the TUI are held in memory only; NodeMop does not write them to the project

Press `g` from Project Safety to generate:

- a short project summary
- a GitHub repository description
- a suggested archive/backup commit message

For the AI context, NodeMop reads project metadata/README/config files plus a source **file map**. It does not read `.env` files or arbitrary source bodies, and lines that look like secrets are redacted from metadata sent to the summarizer.

### Storage cleanup

`x` still performs the v0.2 safe cleanup and can remove only generated directories:

- `node_modules`
- `.next`
- `dist`
- `build`
- `coverage`
- `.cache`
- `.turbo`

Source files and `.git` are never removed by this cleanup flow.

## Requirements

Required:

- Windows 10/11
- Go 1.25+
- Git

Recommended for the Git workflow:

- Lazygit
- GitHub CLI (`gh`) logged into GitHub

Optional AI provider:

- Ollama running locally, **or**
- Gemini API key

## Build

```powershell
cd nodemop
go mod tidy
go build -o nodemop.exe ./cmd/nodemop
```

Run:

```powershell
.\nodemop.exe "C:\Users\YOURNAME\Projects"
```

## Main controls

| Key | Action |
|---|---|
| `↑` / `↓` | Navigate projects |
| `Enter` | Inspect selected project |
| `Space` | Select/unselect |
| `o` | Select old / unsafe Git projects |
| `x` | Clean generated files |
| `a` | AI provider setup |
| `r` | Rescan |
| `q` | Quit |

### Project Safety screen

| Key | Action |
|---|---|
| `i` | Initialize Git + safety `.gitignore` |
| `c` | Create private `bg-*` GitHub remote using `gh` |
| `l` | Open Lazygit in the project |
| `g` | Generate AI Git suggestions |
| `a` | AI settings |
| `Esc` | Back |

## Intended archival flow

```text
scan
  ↓
NO GIT / DIRTY / NO REMOTE / AHEAD
  ↓
inspect
  ↓
optional AI summary
  ↓
git init (if required)
  ↓
create private GitHub remote (if required)
  ↓
Lazygit: review → stage → commit → push
  ↓
return to NodeMop
  ↓
rescan confirms Git state
  ↓
clean generated dependencies/build output
```

## Next milestone

v0.4 should add the final archival guardrail:

- verify local HEAD exists on the remote
- verify no uncommitted files remain
- optional deletion of the complete local project only after both checks pass
- Markdown/CSV archive report
- batch "make my projects safe" queue
- secret-warning pass before GitHub remote creation

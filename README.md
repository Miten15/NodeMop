# NodeMop

> Clean your dev graveyard without losing your code.

NodeMop is an open-source, cross-platform terminal UI for finding forgotten JavaScript/TypeScript projects, making them safe in Git, reclaiming disk space from generated files such as `node_modules`, `.next`, `dist`, and caches, and cleaning unused Docker resources.

It is meant for the project folders most developers eventually accumulate: old React apps, experiments without Git, repos with uncommitted work, local commits that were never pushed, linked Git worktrees, dependency folders taking up gigabytes of storage, and Docker leftovers from old development environments.

## What NodeMop does

### Discover projects

NodeMop recursively scans for `package.json` and currently recognizes:

- Next.js
- React
- React + Vite
- Vue
- Svelte
- Remix
- Vite
- generic Node.js projects

For every project it shows the framework, last activity, Git state, project size, and reclaimable generated-file size.

### Understand Git safety

NodeMop highlights projects as:

- `NO GIT`
- `NO REMOTE`
- `DIRTY`
- `AHEAD`
- `BEHIND`
- `DIVERGED`
- `CLEAN`

Git detection uses Git itself rather than assuming `.git` must be a directory. That means NodeMop also recognizes **linked Git worktrees** and projects located inside a worktree.

From the project details screen you can initialize Git, create a private GitHub remote with `gh`, and open Lazygit directly in the project to review, stage, commit, pull, and push interactively.

NodeMop deliberately does not silently push your code for you.

### AI project helper

NodeMop can optionally generate:

- a short project summary
- a GitHub repository description
- a suggested archive/backup commit message

Supported providers:

- **Ollama** — local models through `http://127.0.0.1:11434`
- **Gemini** — using `GEMINI_API_KEY` or a key entered in the TUI

For summarization, NodeMop reads project metadata, README/config files, and a source file map. It does not intentionally read `.env` files or arbitrary source bodies into the AI context, and secret-looking metadata lines are redacted.

### Reclaim project storage safely

The project cleanup action only removes generated directories:

- `node_modules`
- `.next`
- `dist`
- `build`
- `coverage`
- `.cache`
- `.turbo`

Source files and `.git` are not removed by this cleanup flow.

### Docker Mop

NodeMop also includes a dedicated Docker cleanup TUI:

```bash
nodemop docker
```

On Windows:

```powershell
.\nodemop.exe docker
```

Docker Mop inspects Docker storage and lets you selectively clean:

- stopped containers
- unused images
- build cache
- unused networks

It also shows Docker's current storage and reclaimable-space summary.

**Volumes are protected by default and Docker Mop does not prune them.** Persistent databases and other application data often live in Docker volumes, so NodeMop keeps them outside the normal cleanup path.

Running containers are also left alone.

Docker Mop controls:

| Key | Action |
|---|---|
| `↑` / `↓` or `j` / `k` | Navigate cleanup categories |
| `Space` | Toggle category |
| `a` | Select all safe categories |
| `c` | Clear selection |
| `x` / `Enter` | Review and clean selected categories |
| `r` | Refresh Docker usage |
| `Esc` / `q` | Exit Docker Mop |

## Platform support

NodeMop is designed to run on:

- Windows
- Linux
- macOS

It is written in Go and uses Bubble Tea for the terminal UI.

## Requirements

Required for project scanning:

- Go 1.25+
- Git

Recommended:

- [Lazygit](https://github.com/jesseduffield/lazygit)
- [GitHub CLI](https://cli.github.com/) (`gh auth login`)

Optional AI:

- Ollama, or
- a Gemini API key

Optional Docker cleanup:

- Docker CLI
- a running Docker engine / Docker Desktop

## Build from source

```bash
git clone https://github.com/Miten15/NodeMop.git
cd NodeMop
go mod tidy
go build ./cmd/nodemop
```

### Windows

```powershell
go build -o nodemop.exe ./cmd/nodemop
.\nodemop.exe "C:\Users\YOURNAME\Projects"
```

Docker Mop:

```powershell
.\nodemop.exe docker
```

### Linux / macOS

```bash
go build -o nodemop ./cmd/nodemop
./nodemop "$HOME/projects"
```

Docker Mop:

```bash
./nodemop docker
```

You can point NodeMop at a broad folder such as `Documents`, `Projects`, or a development drive and let it discover projects recursively.

## Controls

### Project list

| Key | Action |
|---|---|
| `↑` / `↓` or `j` / `k` | Navigate |
| `Enter` | Inspect project |
| `Space` | Select/unselect |
| `o` | Select old / unsafe Git projects |
| `x` | Clean generated files |
| `a` | AI provider setup |
| `r` | Rescan |
| `q` | Quit |

### Project safety screen

| Key | Action |
|---|---|
| `i` | Initialize Git + add NodeMop safety ignores |
| `c` | Create a private `bg-*` GitHub remote with `gh` |
| `l` | Open Lazygit in the project |
| `g` | Generate AI Git suggestions |
| `a` | AI settings |
| `Esc` | Back |

## Recommended workflow

```text
scan projects
    ↓
find NO GIT / DIRTY / AHEAD / NO REMOTE
    ↓
inspect project
    ↓
optional AI summary
    ↓
git init if required
    ↓
create private GitHub remote if required
    ↓
open Lazygit
    ↓
review → stage → commit → push
    ↓
return to NodeMop and rescan
    ↓
clean generated dependencies/build output
    ↓
open Docker Mop
    ↓
remove stopped containers / unused images / build cache / networks
```

## Ollama

NodeMop discovers installed Ollama models from:

```text
GET http://127.0.0.1:11434/api/tags
```

A small local model is enough for the current summarization task.

## Roadmap

- verify local `HEAD` exists on the remote before archive/delete actions
- Markdown/CSV cleanup reports
- batch “make my projects safe” workflow
- integrate Docker Mop into the main dashboard with a `d` shortcut
- richer Docker resource inspection before pruning
- better streaming/progress feedback for local LLM generation
- optional full project deletion only after remote verification
- additional developer project types beyond JavaScript/TypeScript

## Safety philosophy

NodeMop should make destructive actions boring and obvious. The intended order is:

**understand → commit → push → verify → clean**

not the other way around.

For Docker, the same principle applies: running containers and volumes are protected from the default cleanup flow.

## License

NodeMop is open source under the [MIT License](LICENSE).

## Contributing

Issues and pull requests are welcome. If you find an edge case involving monorepos, worktrees, package managers, Docker resources, or generated directories, please include the project layout and platform so it can be reproduced safely.

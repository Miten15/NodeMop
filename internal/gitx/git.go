package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/example/nodemop/internal/project"
)

func Installed(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// RepositoryInfo detects Git repositories by asking Git itself instead of
// looking for a .git directory. This is important for linked worktrees, where
// .git is a file at the worktree root, and for projects nested below that root.
func RepositoryInfo(path string) (inside bool, root string, worktree bool) {
	if !Installed("git") {
		return false, "", false
	}
	if output(path, "rev-parse", "--is-inside-work-tree") != "true" {
		return false, "", false
	}

	root = output(path, "rev-parse", "--show-toplevel")
	gitDir := output(path, "rev-parse", "--git-dir")
	commonDir := output(path, "rev-parse", "--git-common-dir")

	// In a linked worktree Git keeps the per-worktree git dir under
	// <common git dir>/worktrees/<name>. Comparing canonical paths also works
	// when Git returns relative paths on one platform and absolute paths on
	// another.
	gitDirAbs := resolveGitPath(path, gitDir)
	commonDirAbs := resolveGitPath(path, commonDir)
	if gitDirAbs != "" && commonDirAbs != "" && !samePath(gitDirAbs, commonDirAbs) {
		worktree = true
	}
	return true, root, worktree
}

func resolveGitPath(base, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return filepath.Clean(abs)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	// Windows paths are case-insensitive in the common/default setup. Using
	// EqualFold is harmless on Linux/macOS here because these paths only come
	// from the same Git invocation and should otherwise match exactly.
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func Inspect(path string) (branch string, changed int, ahead int, behind int, state project.GitState) {
	inside, _, _ := RepositoryInfo(path)
	if !inside {
		return "", 0, 0, 0, project.GitNone
	}

	branch = output(path, "rev-parse", "--abbrev-ref", "HEAD")
	status := output(path, "status", "--porcelain")
	if status != "" {
		changed = len(strings.Split(strings.TrimSpace(status), "\n"))
	}

	remote := output(path, "remote", "get-url", "origin")
	if remote == "" {
		if changed > 0 {
			return branch, changed, 0, 0, project.GitDirty
		}
		return branch, 0, 0, 0, project.GitNoRemote
	}

	counts := output(path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if counts != "" {
		parts := strings.Fields(counts)
		if len(parts) == 2 {
			behind, _ = strconv.Atoi(parts[0])
			ahead, _ = strconv.Atoi(parts[1])
		}
	}

	switch {
	case changed > 0:
		state = project.GitDirty
	case ahead > 0 && behind > 0:
		state = project.GitDiverged
	case ahead > 0:
		state = project.GitAhead
	case behind > 0:
		state = project.GitBehind
	default:
		state = project.GitClean
	}
	return
}

func Init(path string) error {
	if !Installed("git") {
		return errors.New("git is not installed or not in PATH")
	}
	if inside, _, _ := RepositoryInfo(path); inside {
		return nil
	}
	if err := ensureSafeGitignore(path); err != nil {
		return err
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	return cmd.Run()
}

func ensureSafeGitignore(path string) error {
	gitignore := filepath.Join(path, ".gitignore")
	existing := ""
	if b, err := os.ReadFile(gitignore); err == nil {
		existing = string(b)
	}
	entries := []string{
		"node_modules/", ".next/", "dist/", "build/", "coverage/", ".cache/", ".turbo/",
		".env", ".env.*", "!.env.example", "*.log", ".DS_Store", "Thumbs.db",
	}
	var add bytes.Buffer
	for _, entry := range entries {
		if !containsLine(existing, entry) {
			add.WriteString(entry)
			add.WriteByte('\n')
		}
	}
	if add.Len() == 0 {
		return nil
	}
	f, err := os.OpenFile(gitignore, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString("\n# NodeMop safety defaults\n" + add.String())
	return err
}

func containsLine(content, want string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func output(path string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CreatePrivateGitHubRemote creates a private GitHub repository and attaches it
// as origin. It intentionally does NOT push; NodeMop hands commit/push control
// to Lazygit so the user can review exactly what is going upstream.
func CreatePrivateGitHubRemote(path, repoName, description string) error {
	if !Installed("gh") {
		return errors.New("GitHub CLI (gh) is not installed or not in PATH")
	}
	if strings.TrimSpace(repoName) == "" {
		return errors.New("repository name is empty")
	}
	args := []string{"repo", "create", repoName, "--private", "--source", ".", "--remote", "origin"}
	if strings.TrimSpace(description) != "" {
		args = append(args, "--description", description)
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh repo create failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ArchiveRepoName(projectName string) string {
	name := strings.ToLower(strings.TrimSpace(projectName))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if ok {
			if r == '_' {
				r = '-'
			}
			if r == '-' {
				if lastDash {
					continue
				}
				lastDash = true
			} else {
				lastDash = false
			}
			b.WriteRune(r)
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		clean = "project"
	}
	return "bg-" + clean
}

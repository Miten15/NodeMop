package scanner

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Miten15/NodeMop/internal/cleanup"
	"github.com/Miten15/NodeMop/internal/gitx"
	"github.com/Miten15/NodeMop/internal/project"
)

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, "dist": true,
	"build": true, "coverage": true, ".cache": true, ".turbo": true,
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func Scan(root string, oldAfter time.Duration) ([]project.Project, error) {
	root, err := filepath.Abs(root)
	if err != nil { return nil, err }
	if stat, err := os.Stat(root); err != nil || !stat.IsDir() { return nil, errors.New("scan path is not a directory") }

	var projectRoots []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil { return nil }
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] { return filepath.SkipDir }
			return nil
		}
		if d.Name() == "package.json" { projectRoots = append(projectRoots, filepath.Dir(path)) }
		return nil
	})
	if err != nil { return nil, err }

	sort.Strings(projectRoots)
	filtered := make([]string, 0, len(projectRoots))
	for _, candidate := range projectRoots {
		nested := false
		for _, parent := range filtered {
			rel, err := filepath.Rel(parent, candidate)
			if err == nil && rel != "." && !strings.HasPrefix(rel, "..") { nested = true; break }
		}
		if !nested { filtered = append(filtered, candidate) }
	}

	projects := make([]project.Project, len(filtered))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i,p := range filtered {
		wg.Add(1)
		go func(i int, p string) { defer wg.Done(); sem<-struct{}{}; projects[i]=inspect(p,oldAfter); <-sem }(i,p)
	}
	wg.Wait()
	sort.Slice(projects, func(i,j int) bool { return projects[i].LastActivity.Before(projects[j].LastActivity) })
	return projects,nil
}

func inspect(path string, oldAfter time.Duration) project.Project {
	p := project.Project{Name: filepath.Base(path), Path: path, Framework: detectFramework(filepath.Join(path,"package.json"))}
	if inside, gitRoot, worktree := gitx.RepositoryInfo(path); inside {
		p.HasGit=true; p.GitRoot=gitRoot; p.IsWorktree=worktree
		p.LastActivity=gitLastCommit(path); p.RemoteURL=gitRemote(path); p.HasRemote=p.RemoteURL!=""
		p.Branch,p.ChangedFiles,p.Ahead,p.Behind,p.GitState=gitx.Inspect(path); p.HasUncommitted=p.ChangedFiles>0
	} else {
		p.LastActivity=newestMeaningfulFile(path); p.GitState=project.GitNone
	}
	p.SizeBytes,p.NodeModules=directorySizes(path); p.CleanupBytes=cleanup.ReclaimableSize(path)
	if !p.HasGit { p.Status=project.StatusNoGit } else if !p.LastActivity.IsZero() && time.Since(p.LastActivity)<=oldAfter { p.Status=project.StatusActive } else { p.Status=project.StatusOld }
	return p
}

func detectFramework(packagePath string) string {
	f,err:=os.Open(packagePath);if err!=nil{return "Node.js"};defer f.Close();var pkg packageJSON;if json.NewDecoder(f).Decode(&pkg)!=nil{return "Node.js"}
	has:=func(name string)bool{_,a:=pkg.Dependencies[name];_,b:=pkg.DevDependencies[name];return a||b}
	switch {case has("next"):return "Next.js";case has("@remix-run/react"):return "Remix";case has("vue"):return "Vue";case has("svelte")||has("@sveltejs/kit"):return "Svelte";case has("react")&&has("vite"):return "React / Vite";case has("react"):return "React";case has("vite"):return "Vite";default:return "Node.js"}
}

func gitLastCommit(path string) time.Time { cmd:=exec.Command("git","-C",path,"log","-1","--format=%cI");out,err:=cmd.Output();if err!=nil{return newestMeaningfulFile(path)};t,err:=time.Parse(time.RFC3339,strings.TrimSpace(string(out)));if err!=nil{return newestMeaningfulFile(path)};return t }
func gitRemote(path string) string { cmd:=exec.Command("git","-C",path,"remote","get-url","origin");out,err:=cmd.Output();if err!=nil{return ""};return strings.TrimSpace(string(out)) }

func newestMeaningfulFile(root string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(root, func(path string,d fs.DirEntry,err error)error{if err!=nil{return nil};if d.IsDir(){if path!=root&&ignoredDirs[d.Name()]{return filepath.SkipDir};return nil};info,err:=d.Info();if err==nil&&info.ModTime().After(newest){newest=info.ModTime()};return nil})
	return newest
}

func directorySizes(root string)(total int64,nodeModules int64){_ = filepath.WalkDir(root,func(path string,d fs.DirEntry,err error)error{if err!=nil{return nil};if d.IsDir(){if d.Name()==".git"{return filepath.SkipDir};return nil};info,err:=d.Info();if err!=nil{return nil};total+=info.Size();for _,part:=range strings.Split(filepath.Clean(path),string(os.PathSeparator)){if part=="node_modules"{nodeModules+=info.Size();break}};return nil});return}

func ReadGitignore(path string)([]string,error){f,err:=os.Open(filepath.Join(path,".gitignore"));if err!=nil{return nil,err};defer f.Close();var lines []string;s:=bufio.NewScanner(f);for s.Scan(){lines=append(lines,s.Text())};return lines,s.Err()}

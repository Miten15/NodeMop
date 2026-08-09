package project

import "time"

type Status string

const (
	StatusActive Status = "ACTIVE"
	StatusOld    Status = "OLD"
	StatusNoGit  Status = "NO GIT"
)

type GitState string

const (
	GitNone     GitState = "NO GIT"
	GitClean    GitState = "CLEAN"
	GitDirty    GitState = "DIRTY"
	GitNoRemote GitState = "NO REMOTE"
	GitAhead    GitState = "AHEAD"
	GitBehind   GitState = "BEHIND"
	GitDiverged GitState = "DIVERGED"
)

type Project struct {
	Name           string
	Path           string
	Framework      string
	HasGit         bool
	GitRoot        string
	IsWorktree     bool
	HasRemote      bool
	RemoteURL      string
	Branch         string
	HasUncommitted bool
	ChangedFiles   int
	Ahead          int
	Behind         int
	GitState       GitState
	LastActivity   time.Time
	SizeBytes      int64
	NodeModules    int64
	CleanupBytes   int64
	Status         Status
}

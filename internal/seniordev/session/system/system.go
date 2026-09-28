//go:build !windows

// Package system builds the environment block of a turn's system prompt:
// the model in use, the working directory and the date.
package system

import (
	"runtime"
	"time"
)

type API struct {
	ID string `json:"id"`
}

type Model struct {
	ProviderID string `json:"providerID"`
	API        API    `json:"api"`
}

type Project struct {
	VCS string `json:"vcs"`
}

type Context struct {
	Directory string  `json:"directory"`
	Worktree  string  `json:"worktree"`
	Project   Project `json:"project"`
}

type Service struct {
	Context  Context
	Now      func() time.Time
	Platform string
}

func New(context Context) *Service {
	return &Service{Context: context, Now: time.Now, Platform: platformName()}
}

func (service *Service) Environment(model Model) []string {
	now := service.Now
	if now == nil {
		now = time.Now
	}
	platform := service.Platform
	if platform == "" {
		platform = platformName()
	}
	return BuildEnvironment(model, service.Context, now(), platform)
}

func BuildEnvironment(
	model Model, context Context, now time.Time, platform string,
) []string {
	isGit := "no"
	if context.Project.VCS == "git" {
		isGit = "yes"
	}
	return []string{
		"You are powered by the model named " + model.API.ID +
			". The exact model ID is " + model.ProviderID + "/" + model.API.ID + "\n" +
			"Here is some useful information about the environment you are running in:\n" +
			"<env>\n" +
			"  Working directory: " + context.Directory + "\n" +
			"  Workspace root folder: " + context.Worktree + "\n" +
			"  Is directory a git repo: " + isGit + "\n" +
			"  Platform: " + platform + "\n" +
			"  Today's date: " + now.Format("Mon Jan 02 2006") + "\n" +
			"</env>",
	}
}

// platformName is the OS label shown in the prompt; Windows is reported as
// win32.
func platformName() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

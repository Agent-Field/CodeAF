// Package system ports src/session/system.ts:1-73 from swe-pro commit
// 3b25a1a.
package system

import (
	"runtime"
	"strings"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/assets"
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

func Provider(model Model) []string {
	id := model.API.ID
	path := "src/session/prompt/default.txt"
	if strings.Contains(id, "gpt-4") ||
		strings.Contains(id, "o1") ||
		strings.Contains(id, "o3") {
		path = "src/session/prompt/beast.txt"
	} else if strings.Contains(id, "gpt") {
		if strings.Contains(id, "codex") {
			path = "src/session/prompt/codex.txt"
		} else {
			path = "src/session/prompt/gpt.txt"
		}
	} else if strings.Contains(id, "gemini-") {
		path = "src/session/prompt/gemini.txt"
	} else if strings.Contains(id, "claude") {
		path = "src/session/prompt/anthropic.txt"
	} else if strings.Contains(strings.ToLower(id), "trinity") {
		path = "src/session/prompt/trinity.txt"
	} else if strings.Contains(strings.ToLower(id), "kimi") {
		path = "src/session/prompt/kimi.txt"
	}
	prompt, ok := assets.Get(path)
	if !ok {
		panic("system: missing embedded prompt " + path)
	}
	return []string{prompt}
}

type Service struct {
	Context  Context
	Now      func() time.Time
	Platform string
}

func New(context Context) *Service {
	return &Service{Context: context, Now: time.Now, Platform: nodePlatform()}
}

func (service *Service) Environment(model Model) []string {
	now := service.Now
	if now == nil {
		now = time.Now
	}
	platform := service.Platform
	if platform == "" {
		platform = nodePlatform()
	}
	return BuildEnvironment(model, service.Context, now(), platform)
}

func (service *Service) Skills(_ any) *string { return nil }

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

func nodePlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

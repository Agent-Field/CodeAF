package main

// seed.go writes the demo home.
//
// The order below is the order the surface reads things in: the projects on the
// disk, the conversations in their buckets, the work each project ran, the
// standing orders and what they have cost, what the machine remembers, what it
// has spent, and the messages a search ranks. Every one of them goes through
// the writer the engine itself uses, so the surface is reading its own output
// and not a second author's idea of the shape.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// firstProjectName is the project a launch opens in, so home's foot says `here
// ~/aforge-v2` rather than naming whatever directory the make target was run
// from. It is the first row of [demoProjects] and is spelled once.
const firstProjectName = "aforge-v2"

// builtHome is what one seeding came to — the counts, so the program can say
// what it made and the test can insist none of them is zero.
type builtHome struct {
	Dir           string
	Projects      int
	Conversations int
	Tasks         int
	Standing      int
	Memories      int
	UsageLines    int
	Messages      int
	Artifacts     int
}

func (b builtHome) line() string {
	return fmt.Sprintf("built a demo home in %s: %d projects, %d conversations, %d pieces of work, "+
		"%d standing orders, %d memories, %d spending lines, %d messages, %d made things",
		b.Dir, b.Projects, b.Conversations, b.Tasks, b.Standing, b.Memories, b.UsageLines, b.Messages, b.Artifacts)
}

// seedDemoHome writes a whole v3 state into dir and answers what it wrote.
//
// dir is treated as a HOME: the projects are folders directly under it, exactly
// as a person's are, and the state root is dir/.aforge. THAT IS NOT A STYLE
// CHOICE. A project under /tmp is litter to the launch sweep (internal/session's
// sweep.go marks a temp-rooted session reapable after a week), so a fixture that
// put its projects in a temporary directory would watch its own oldest
// conversations disappear on the first launch.
func seedDemoHome(dir string, now time.Time) (builtHome, error) {
	built := builtHome{Dir: dir}
	root := filepath.Join(dir, ".aforge")

	projects, err := writeProjects(dir, now)
	if err != nil {
		return built, err
	}
	built.Projects = len(projects)

	brain, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		return built, fmt.Errorf("open the memory store: %w", err)
	}
	defer brain.Close()

	// Every conversation's id by its title, so the rows that are ABOUT a
	// conversation — a task the project's index remembers, a line in the
	// spending ledger — name one that is really on the disk. A join that points
	// at nothing draws a row a person cannot open.
	ids := map[string]string{}
	for _, talk := range demoConversations {
		project, ok := projects[talk.project]
		if !ok {
			return built, fmt.Errorf("conversation %q names no project %q", talk.title, talk.project)
		}
		id, err := writeConversation(project, talk, now, brain)
		if err != nil {
			return built, err
		}
		ids[talk.title] = id
		built.Conversations++
		built.Messages += 2 * len(talk.turns)
	}

	tasks, err := writeTaskIndex(projects, ids, now)
	if err != nil {
		return built, err
	}
	built.Tasks = tasks

	orders, err := writeStanding(filepath.Join(root, "v3", "standing"), projects, now)
	if err != nil {
		return built, err
	}
	built.Standing = orders

	memories, err := writeMemories(brain)
	if err != nil {
		return built, err
	}
	built.Memories = memories

	lines, err := writeUsage(filepath.Join(root, "v3", session.UsageLedgerName), projects, ids, now)
	if err != nil {
		return built, err
	}
	built.UsageLines = lines

	built.Artifacts = writeArtifacts(filepath.Join(root, "v3", session.ArtifactsIndexName), projects, ids, now)
	return built, nil
}

// ── the projects ────────────────────────────────────────────────────────────

// demoProject is one workspace on the demo machine.
type demoProject struct {
	// name is the folder under the demo HOME, and what home draws as the
	// project's name (internal/session's projectName takes the last element of
	// the recorded path).
	name string
	// git says this workspace is a real repository with an uncommitted change in
	// it. Home's repo band runs `git status --porcelain=v2 --branch` against the
	// workspace and draws NOTHING at all when git cannot answer
	// (internal/tui3/homeband_repo.go), so at least one project has to be one or
	// that whole band is invisible on the demo.
	git bool
	// files are the workspace's contents, so a project the surface names is a
	// project that is actually there — a row whose folder is gone loses three of
	// its card's keys and says `folder gone`.
	files map[string]string
	// dirty is written AFTER the first commit and never committed, which is what
	// makes the band's second clause say how many files are uncommitted.
	dirty map[string]string

	dir    string // filled in by writeProjects
	bucket string
}

var demoProjects = []demoProject{
	{
		name: firstProjectName,
		git:  true,
		files: map[string]string{
			"README.md":       "# aforge-v2\n\nThe surface, the engine, and the manual that keeps them honest.\n",
			"Makefile":        "build:\n\tgo build -o bin/aforge ./cmd/aforge\n",
			"docs/HOME.md":    "# Home\n\nOne list, ordered by what wants you first.\n",
			"internal/why.go": "package internal\n\n// Why the frame jumps: the card is measured before the list is folded.\n",
		},
		dirty: map[string]string{
			"README.md":  "# aforge-v2\n\nThe surface, the engine, and the manual that keeps them honest.\n\nThe tab bar's counts are drawn from one reading.\n",
			"scratch.md": "counting the tabs — the bar reads the world once and every place shares it\n",
		},
	},
	{
		name: "pricing-site",
		files: map[string]string{
			"index.html":      "<h1>Pricing</h1>\n",
			"pricing.json":    "{\"seat\": 19, \"enterprise\": 12}\n",
			"notes/ladder.md": "The enterprise ladder beats per-seat above forty users.\n",
		},
	},
	{
		name: "infra",
		files: map[string]string{
			"terraform/main.tf":  "resource \"aws_acm_certificate\" \"wildcard\" {}\n",
			"runbooks/certs.md":  "The wildcard certificate rotates on the first of the month.\n",
			"runbooks/backup.md": "The backup window is four hours and wants to be twenty minutes.\n",
		},
	},
}

// writeProjects lays the three workspaces down under the demo HOME and answers
// them by name, each with its folder and its bucket resolved.
func writeProjects(dir string, now time.Time) (map[string]*demoProject, error) {
	byName := map[string]*demoProject{}
	for index := range demoProjects {
		project := demoProjects[index]
		project.dir = filepath.Join(dir, project.name)
		project.bucket = filepath.Join(dir, ".aforge", "v3", "projects", encodeWorkspace(project.dir))
		if err := os.MkdirAll(project.bucket, 0o700); err != nil {
			return nil, fmt.Errorf("make %s: %w", project.bucket, err)
		}
		if err := writeTree(project.dir, project.files); err != nil {
			return nil, err
		}
		if project.git {
			if err := initRepository(project.dir, now); err != nil {
				return nil, err
			}
			if err := writeTree(project.dir, project.dirty); err != nil {
				return nil, err
			}
		}
		byName[project.name] = &project
	}
	return byName, nil
}

// writeTree writes a workspace's files, making the directories they need.
func writeTree(dir string, files map[string]string) error {
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// initRepository makes one workspace a real git repository with one commit on
// it, so `git status --branch` has a branch to name.
//
// THE IDENTITY IS PASSED PER COMMAND AND NEVER WRITTEN ANYWHERE. `git -c` sets
// it for the one invocation; a `git config --global` would reach outside the
// directory this program was given, which is the one thing it must never do.
func initRepository(dir string, now time.Time) error {
	stamp := now.Format(time.RFC3339)
	for _, argv := range [][]string{
		{"init", "--initial-branch=master", "--quiet"},
		{"add", "."},
		{"-c", "user.name=Demo", "-c", "user.email=demo@example.invalid", "commit", "--quiet", "-m", "the first commit"},
	} {
		command := exec.Command("git", append([]string{"-C", dir}, argv...)...)
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
			// A demo home must not inherit the machine's git configuration:
			// a global hooksPath or commit.gpgsign would fail a commit here
			// for a reason that has nothing to do with the demo.
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s in %s: %w: %s", strings.Join(argv, " "), dir, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// encodeWorkspace is the bucket name for a workspace: the path with its
// separators turned into dashes.
//
// IT IS A COPY, DELIBERATELY, AND IT IS THE THIRD ONE. The encoder lives in
// cmd/aforge's chatv3_layout.go and is unexported there, and internal/standing's
// inbox.go and internal/e2e's tmux test already spell it for themselves. What
// makes a copy safe here is that the encoding is ONE-WAY and nothing reads it
// back: the world layer takes a project's real path out of each session's
// meta.json and never decodes a bucket (internal/session's world.go). A drift
// would show up as a duplicate row on home the moment the surface opened one of
// these projects, which is what the test beside this program looks for.
func encodeWorkspace(workspace string) string {
	encoded := strings.ReplaceAll(filepath.Clean(workspace), string(filepath.Separator), "-")
	encoded = strings.ReplaceAll(encoded, ":", "-")
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return encoded
}

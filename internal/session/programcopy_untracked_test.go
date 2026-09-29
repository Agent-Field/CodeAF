package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
)

// THE ENDING'S STASH COMMAND MUST TAKE ONLY THE INPUTS IT NAMES, because git
// reads shell-quoted brackets and stars as pathspec patterns after the shell
// has handed it each word.
func TestTheEndingsStashTakesOnlyTheFilesItNames(t *testing.T) {
	repo := newTestRepo(t)
	inputs := []string{"notes[1].md", "notes1.md", "star*.txt", "starZ.txt", "it's two  spaces.txt"}
	for _, path := range inputs {
		writeFile(t, filepath.Join(repo, path), "person's "+path+"\n")
	}
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Finish the notes")
	want := []string{"notes[1].md", "star*.txt", "it's two  spaces.txt"}
	for _, path := range want {
		writeFile(t, filepath.Join(folder.Dir, path), "run's "+path+"\n")
	}
	end := folder.Finish("done")
	sentence := end.Sentence()
	prefix := "`git -C " + shellQuoted(repo) + " stash push -u -- "
	start := strings.Index(sentence, prefix)
	if start < 0 {
		t.Fatalf("the ending has no stash push command: %s", sentence)
	}
	command := sentence[start+1:]
	stop := strings.IndexByte(command, '`')
	if stop < 0 {
		t.Fatalf("the ending does not close its stash push command: %s", sentence)
	}
	command = command[:stop]
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ending command %q: %v\n%s", command, err, out)
	}
	stashed := strings.TrimSuffix(gitOut(t, repo, "show", "--name-only", "--format=", "-z", "stash@{0}^3"), "\x00")
	got := strings.Split(stashed, "\x00")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the ending's stash took %q, want only %q; command: %s", got, want, command)
	}
	for _, path := range []string{"notes1.md", "starZ.txt"} {
		if got := readFile(t, filepath.Join(repo, path)); got != "person's "+path+"\n" {
			t.Fatalf("the ending changed untouched input %q to %q", path, got)
		}
	}
	mustGit(t, repo, "merge", folder.Branch)
}

// THE FINISHING COMMIT MUST STAGE ONLY THE INPUT IT CHANGED. A bracketed name
// otherwise matches its unbracketed neighbour as a git pathspec and writes the
// untouched input's bytes into the object store even if it leaves the branch.
func TestTheFinishingCommitWritesNoBlobForAnInputLeftAlone(t *testing.T) {
	repo := newTestRepo(t)
	for _, name := range []string{"notes[1].md", "notes1.md"} {
		writeFile(t, filepath.Join(repo, name), "person's "+name+"\n")
	}
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Finish the notes")
	writeFile(t, filepath.Join(folder.Dir, "notes[1].md"), "run's notes\n")
	end := folder.Finish("done")
	if !end.Kept || end.Refused != "" {
		t.Fatalf("finishing the changed input: %+v", end)
	}
	if got := gitOut(t, repo, "ls-tree", "-r", "--name-only", "-z", folder.Branch); got != "notes[1].md\x00shared.txt\x00" {
		t.Fatalf("finished branch tree = %q, want only the changed input and shared.txt", got)
	}
	blob := strings.TrimSpace(gitOut(t, repo, "hash-object", "--", "notes1.md"))
	if _, err := git(repo, "cat-file", "-e", blob); err == nil {
		t.Fatal("the input left alone was written into git's object store")
	}
}

// AN UNTRACKED FILE THE RUN CHANGED IS ITS WORK; ONE IT LEFT ALONE IS NOT. The
// person's untracked files are copied in as inputs without entering git; a
// half-written parser.py the run finishes goes on its branch, and a
// credentials.json or a large file it never touched stays off it and out of
// git's object store — at an ordinary ending and at recovery from a codeaf that
// went away alike, while staged additions and the program's new files ship.
func TestProgramUntrackedInputsTheRunChangedAreItsWork(t *testing.T) {
	for _, crashed := range []bool{false, true} {
		name := "finished"
		if crashed {
			name = "recovered"
		}
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			writeFile(t, filepath.Join(repo, "shared.txt"), "tracked edit\n")
			writeFile(t, filepath.Join(repo, "staged.go"), "package staged\n")
			mustGit(t, repo, "add", "staged.go")
			left := []string{"credentials.json", "local/large.bin", "local/odd\n[1].txt"}
			for _, path := range left {
				writeFile(t, filepath.Join(repo, path), "local input: "+path+strings.Repeat("x", 65536))
			}
			writeFile(t, filepath.Join(repo, "parser.py"), "def parse_numbers(text):\n    raise NotImplementedError\n")
			status := gitOut(t, repo, "status", "--porcelain", "-z")
			folder := prepareIn(t, testPrograms("fake")[0], repo, "Finish parse_numbers")
			for _, path := range append(left, "parser.py") {
				if got := readFile(t, filepath.Join(folder.Dir, path)); got != readFile(t, filepath.Join(repo, path)) {
					t.Fatalf("%q was not copied intact", path)
				}
				if !folder.excludedFromCommit(path) {
					t.Fatalf("input %q is not kept out while it is as it was copied", path)
				}
			}
			if !strings.Contains(folder.LeftBehindWords(), "any it changes are committed as its work, and the rest stay off its branch") {
				t.Fatalf("the receipt misstates what becomes of the inputs: %s", folder.LeftBehindWords())
			}
			if tree := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); tree != "shared.txt\nstaged.go\n" {
				t.Fatalf("the branch begins with %q", tree)
			}
			// THE RUN FINISHES THE PARSER, WRITES THE SAME BYTES OVER THE
			// CREDENTIALS, and adds a file of its own.
			finished := "def parse_numbers(text):\n    return [int(n) for n in text.split()]\n"
			writeFile(t, filepath.Join(folder.Dir, "parser.py"), finished)
			writeFile(t, filepath.Join(folder.Dir, "credentials.json"), readFile(t, filepath.Join(repo, "credentials.json")))
			writeFile(t, filepath.Join(folder.Dir, "test_parser.py"), "from parser import parse_numbers\n")
			var end ProgramFolderEnd
			if crashed {
				folder.release()
				sweepProgramCopies(repo)
			} else {
				end = folder.Finish("done")
			}
			if tree := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); tree != "parser.py\nshared.txt\nstaged.go\ntest_parser.py\n" {
				t.Fatalf("the finished branch holds %q, want the finished parser and none of the inputs it left alone", tree)
			}
			if got := gitOut(t, repo, "show", folder.Branch+":parser.py"); got != finished {
				t.Fatalf("the branch's parser.py = %q, want the run's", got)
			}
			for _, path := range left {
				blob := strings.TrimSpace(gitOut(t, repo, "hash-object", "--", path))
				if _, err := git(repo, "cat-file", "-e", blob); err == nil {
					t.Fatalf("input %q, which the run left alone, was written into git's object store", path)
				}
			}
			if got := gitOut(t, repo, "status", "--porcelain", "-z"); got != status {
				t.Fatalf("the person's index or files changed: %q, want %q", got, status)
			}
			if _, err := os.Stat(folder.Dir); !os.IsNotExist(err) {
				t.Fatalf("the finished copy remains: %v", err)
			}
			if crashed {
				return
			}
			// THE ENDING'S OWN ADVICE BRINGS THE WORK IN, and leaves the inputs
			// the run did not touch where they are.
			stash := "`git -C " + shellQuoted(repo) + " stash` then `git -C " + shellQuoted(repo) + " stash push -u -- 'parser.py'`"
			if sentence := end.Sentence(); !strings.Contains(sentence, "your untracked parser.py") || !strings.Contains(sentence, stash) {
				t.Fatalf("the ending does not say how to put the parser aside:\n%s", sentence)
			}
			mustGit(t, repo, "stash", "-q")
			mustGit(t, repo, "stash", "push", "-q", "-u", "--", "parser.py")
			mustGit(t, repo, "merge", "-q", folder.Branch)
			if got := readFile(t, filepath.Join(repo, "parser.py")); got != finished {
				t.Fatalf("after the ending's merge parser.py = %q, want the run's", got)
			}
			for _, path := range left {
				if _, err := os.Stat(filepath.Join(repo, path)); err != nil {
					t.Fatalf("the ending's stash took input %q, which the branch does not hold: %v", path, err)
				}
			}
		})
	}
}

// UNTRACKED INPUTS ALONE MAKE NO COMMIT: they are not a snapshot, and a run
// that leaves them as they were changed nothing.
func TestOnlyUntrackedInputsMakeNoSnapshotCommit(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "credentials.json"), "local input\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Read the input")
	if folder.Snapshot != "" || branchCommit(repo, folder.Branch) != folder.Start {
		t.Fatal("untracked inputs alone made a snapshot commit")
	}
	if got := readFile(t, filepath.Join(folder.Dir, "credentials.json")); got != "local input\n" {
		t.Fatalf("local input = %q", got)
	}
	if end := folder.Finish("done"); end.Kept || branchCommit(repo, folder.Branch) != folder.Start {
		t.Fatalf("an unchanged input became a finishing commit: %+v", end)
	}
}

// THE CHILD READS THE SAME INPUTS: the list codeaf hands the program names each
// input with the fingerprint it was copied in with, so its own commits draw the
// line where codeaf's finishing commit draws it.
func TestTheProgramIsHandedItsInputsWithTheirFingerprints(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "parser.py"), "half\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Finish it")
	inputs, err := gitidentity.ReadInputs(folder.InputsFile())
	if err != nil || len(inputs) != 1 || inputs["parser.py"] == "" {
		t.Fatalf("the program was handed %v (%v), want parser.py with its fingerprint", inputs, err)
	}
	if !inputs.LeftAlone(folder.Dir, "parser.py") {
		t.Fatal("the handed list reads an untouched input as changed")
	}
	writeFile(t, filepath.Join(folder.Dir, "parser.py"), "finished\n")
	if inputs.LeftAlone(folder.Dir, "parser.py") || folder.excludedFromCommit("parser.py") {
		t.Fatal("an input the run changed is still kept out")
	}
}

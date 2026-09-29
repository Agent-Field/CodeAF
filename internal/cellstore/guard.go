package cellstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// Guard is law L3: a secret never enters a sealed tree. Before each seal it
// scans what the seal will capture. An environment file goes to the vault and
// out of the tree; any other file that holds a secret goes out of the tree with
// one notice. "Out of the tree" is the engine's own exclude policy (withheld.go),
// so the file stays where it is on this machine and is simply never captured.
// Nothing is written into the tree: the exclusions live in the cell's own folder.
//
// The zero Guard works: the vault lives in the codeaf home and a notice is one
// line on stderr.
type Guard struct {
	// Home is the codeaf home that holds the vault; empty means the state root.
	Home string
	// Notify receives one line for a path newly withheld; nil writes to stderr.
	Notify func(string)
	// Ledger, when set, spares a file that has not changed since it was found
	// clean from being read again; EngineFor sets one per session.
	Ledger *keys.Ledger
}

func (g Guard) home() string {
	if g.Home == "" {
		return home.Dir()
	}
	return g.Home
}

func (g Guard) notify(line string) {
	if g.Notify != nil {
		g.Notify(line)
		return
	}
	fmt.Fprintln(os.Stderr, "codeaf: "+line)
}

// Screen is the pre-seal step for the tree the seal will capture. changed is
// the paths the seal will visit, nil for all of it. A path already withheld is
// looked at again every time, so a file is never re-announced and never
// forgotten. A failure to record the exclusion fails the seal: nothing is
// captured that was not screened.
func (g Guard) Screen(c cell.Cell, tree, policyDir string, changed []string) error {
	policy, err := readPolicy(tree, policyDir)
	if err != nil {
		return fmt.Errorf("screen for secrets: %w", err)
	}
	found := g.scan(tree, changed, policy)
	next := policy.with(pathsOf(found))
	if err := next.write(policyDir); err != nil {
		return fmt.Errorf("screen for secrets: %w", err)
	}
	g.announce(policy, found)
	g.vault(c, tree, found)
	return nil
}

// scan is the findings among what a seal of changed would capture, plus the
// paths already withheld.
func (g Guard) scan(tree string, changed []string, policy policyFile) []keys.Finding {
	held := keys.Scanner{Skip: policy.isLeftOut}.Paths(tree, policy.withheld)
	sc := keys.Scanner{Skip: skipping(policy), Ledger: g.Ledger}
	if changed == nil || len(changed) > maxChangedArgs {
		return append(held, sc.Walk(tree)...)
	}
	return append(held, sc.Paths(tree, changed)...)
}

func skipping(policy policyFile) func(string) bool {
	return func(rel string) bool { return policy.isLeftOut(rel) || policy.isWithheld(rel) }
}

func pathsOf(found []keys.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range found {
		if !seen[f.Path] {
			seen[f.Path] = true
			out = append(out, f.Path)
		}
	}
	return out
}

// announce tells a person once about each path newly withheld. It names the
// file and the rule, never the value.
func (g Guard) announce(before policyFile, found []keys.Finding) {
	for _, f := range found {
		if !before.isWithheld(f.Path) {
			g.notify(noticeFor(f))
		}
	}
}

func noticeFor(f keys.Finding) string {
	if f.Rule == keys.RuleDotenv {
		return f.Path + " is kept in your key vault and out of the saved history; your tools still see its variables"
	}
	return f.Path + " looks like it holds a secret (" + f.Rule + "), so it is left out of the saved history; move the secret to an environment variable to save the file"
}

// vault keeps the variables of every environment file found. A vault that
// cannot be written is told to the person; the file is still withheld.
func (g Guard) vault(c cell.Cell, tree string, found []keys.Finding) {
	var files []string
	for _, f := range found {
		if f.Rule == keys.RuleDotenv {
			files = append(files, f.Path)
		}
	}
	if len(files) == 0 {
		return
	}
	if err := g.importDotenv(scopeOf(c), tree, files); err != nil {
		g.notify("could not keep " + strings.Join(files, ", ") + " in the key vault: " + err.Error())
	}
}

func (g Guard) importDotenv(scope, tree string, files []string) error {
	v, err := keys.Open(g.home())
	if err != nil {
		return err
	}
	for _, rel := range files {
		if _, err := v.ImportDotenv(filepath.Join(tree, rel), scope); err != nil {
			return err
		}
	}
	return nil
}

// vaultEnv is the executor's view of the vault: the variables of one cell's
// project.
type vaultEnv struct{ cell cell.Cell }

// Env implements executor.SecretSource.
func (v vaultEnv) Env() []string { return Guard{}.Env(v.cell) }

// Env is the vault's variables for the cell's project, for a tool's
// environment on this machine. A machine with no vault has none.
func (g Guard) Env(c cell.Cell) []string {
	if !keys.Exists(g.home()) {
		return nil
	}
	v, err := keys.Open(g.home())
	if err != nil {
		return nil
	}
	env, _ := v.Env(scopeOf(c))
	return env
}

// scopeOf names the project a cell's secrets belong to (SCHEMAS.md vault
// rulings): the normalized remote it was based on, else the cell's own id.
func scopeOf(c cell.Cell) string {
	if base := c.Meta().Base; base != nil && base.Remote != "" {
		return normalizeRemote(base.Remote)
	}
	return c.ID
}

// normalizeRemote makes the same repository the same scope however it was
// cloned: no scheme, no credentials, no .git suffix, host in lower case.
func normalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if _, rest, ok := strings.Cut(r, "://"); ok {
		r = rest
	}
	if _, rest, ok := strings.Cut(r, "@"); ok {
		r = rest
	}
	host, path, _ := strings.Cut(strings.Replace(r, ":", "/", 1), "/")
	return strings.ToLower(host) + "/" + strings.TrimSuffix(strings.Trim(path, "/"), ".git")
}

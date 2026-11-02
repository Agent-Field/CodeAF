package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const landAttributeReadMax = 8 << 20

// landFilters binds the attributes git will use before status or merge-tree
// can run a filter. Status compares the tracked checkout too, so those paths
// join the branch's additions, modifications, deletions and rename endpoints.
func landFilters(ctx context.Context, plan *TaskBranchLandPlan) error {
	if len(landFilterDrivers(plan.settings)) == 0 {
		plan.attributes = ""
		return nil
	}
	paths, err := landFilterPaths(ctx, plan.Root, plan.Base, plan.BranchHead)
	if err != nil {
		return err
	}
	plan.attributes, err = landFilterAttributes(ctx, plan.Root, plan.settings, paths, "", plan.Head, plan.BranchHead, plan.Base)
	return err
}

func landFilterPaths(ctx context.Context, root, base, branch string) ([]string, error) {
	paths := map[string]bool{}
	for _, args := range [][]string{
		{"ls-files", "--cached", "-z"},
		{"diff", "--no-ext-diff", "--no-textconv", "--name-only", "--no-renames", "-z", base, branch, "--"},
	} {
		out, code, err := landRead(ctx, root, landAttributeReadMax, args...)
		if err != nil || code != 0 || len(out) > 0 && out[len(out)-1] != 0 {
			return nil, landReadFailed(err, refuseLand(LandFilter, "this land cannot determine which files run a filter program"))
		}
		for _, path := range strings.Split(string(out), "\x00") {
			if path != "" {
				paths[path] = true
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered, nil
}

// landFilterAttributes asks git itself, preserving attribute precedence,
// macros, nested files, info/attributes and core.attributesFile. The checkout
// and each tree are separate readings: either side may introduce a filter or
// remove an attribute that still governs a path compared against the base.
func landFilterAttributes(ctx context.Context, root string, config landConfigFacts, paths []string, sources ...string) (string, error) {
	drivers := landFilterDrivers(config)
	if len(drivers) == 0 {
		return "", nil
	}
	input := []byte(strings.Join(paths, "\x00"))
	if len(paths) > 0 {
		input = append(input, 0)
	}
	sum := sha256.New()
	if err := landAttributeFiles(ctx, root, paths, sum); err != nil {
		return "", err
	}
	filtered := map[string]bool{}
	for _, source := range sources {
		out, err := landFilterReading(ctx, root, input, paths, source, drivers, filtered)
		if err != nil {
			return "", err
		}
		sum.Write(out)
	}
	var refused []string
	for _, path := range paths {
		if filtered[path] {
			refused = append(refused, path)
		}
	}
	if len(refused) > 0 {
		return "", refuseLand(LandFilter, "this repository runs a filter program on its files, and a land cannot vouch for it", refused...)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// landFilterReading is one reading of the filter attribute for every path, in
// the checkout (source "") or in one tree. It marks each path a driver with a
// program governs, and answers git's own output so the plan binds it. Output
// that does not name every path, in order, is a reading nobody can vouch for.
func landFilterReading(ctx context.Context, root string, input []byte, paths []string, source string, drivers, filtered map[string]bool) ([]byte, error) {
	args := []string{"check-attr", "-z", "--stdin"}
	if source != "" {
		args = append(args, "--source="+source)
	}
	args = append(args, "filter")
	out, diagnostics, code, err := landReadInput(ctx, root, landAttributeReadMax, input, args...)
	unknown := refuseLand(LandFilter, "this land cannot determine which files run a filter program")
	if err != nil || code != 0 || len(diagnostics) != 0 {
		return nil, landReadFailed(err, unknown)
	}
	records := bytes.Split(out, []byte{0})
	if len(records) != len(paths)*3+1 || len(records[len(records)-1]) != 0 {
		return nil, unknown
	}
	for i, path := range paths {
		if string(records[3*i]) != path || string(records[3*i+1]) != "filter" {
			return nil, unknown
		}
		if drivers[string(records[3*i+2])] {
			filtered[path] = true
		}
	}
	return out, nil
}

// landAttributeFiles binds readable attribute files outside the committed
// trees. Git sometimes silently ignores a read failure (a directory used as
// core.attributesFile, for example), so a successful check-attr is not enough.
func landAttributeFiles(ctx context.Context, root string, paths []string, sum io.Writer) error {
	unknown := refuseLand(LandFilter, "this land cannot determine which files run a filter program")
	files, err := landGitAttributeFiles(ctx, root, sum, unknown)
	if err != nil {
		return err
	}
	landNestedAttributeFiles(files, root, paths)
	ordered := make([]string, 0, len(files))
	for file := range files {
		ordered = append(ordered, file)
	}
	sort.Strings(ordered)
	left := int64(landAttributeReadMax)
	for _, path := range ordered {
		if err := landBindAttributeFile(path, sum, &left, unknown); err != nil {
			return err
		}
	}
	return nil
}

// landGitAttributeFiles is the global, system and info/attributes files git
// names for this repository, as absolute paths.
func landGitAttributeFiles(ctx context.Context, root string, sum io.Writer, unknown error) (map[string]bool, error) {
	files := map[string]bool{}
	for _, args := range [][]string{
		{"var", "GIT_ATTR_GLOBAL"},
		{"var", "GIT_ATTR_SYSTEM"},
		{"rev-parse", "--git-path", "info/attributes"},
	} {
		out, diagnostics, code, err := landReadInput(ctx, root, 64<<10, nil, args...)
		if err != nil || code != 0 || len(diagnostics) != 0 || !bytes.HasSuffix(out, []byte{'\n'}) {
			return nil, landReadFailed(err, unknown)
		}
		// Only the final newline belongs to git; whitespace can name a file.
		path := string(out[:len(out)-1])
		if path == "" {
			if args[0] != "var" {
				return nil, unknown
			}
			// An empty core.attributesFile explicitly disables the global file.
			fmt.Fprintf(sum, "%s:disabled;", args[1])
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		files[path] = true
	}
	return files, nil
}

// landNestedAttributeFiles adds the .gitattributes of every directory from each
// path up to the root of the checkout.
func landNestedAttributeFiles(files map[string]bool, root string, paths []string) {
	for _, path := range paths {
		for dir := filepath.Dir(filepath.FromSlash(path)); ; dir = filepath.Dir(dir) {
			file := filepath.Join(root, dir, ".gitattributes")
			if files[file] {
				break
			}
			files[file] = true
			if dir == "." {
				break
			}
		}
	}
}

// landBindAttributeFile writes one attribute file's path and contents, or that
// it is missing, into the sum. Every file comes out of the one read budget left.
func landBindAttributeFile(path string, sum io.Writer, left *int64, unknown error) error {
	fmt.Fprintf(sum, "%d:%s;", len(path), path)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		fmt.Fprint(sum, "missing;")
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return unknown
	}
	if info.Size() > *left {
		return refuseLand(LandTooLarge, "this land's attribute files are larger than it reads")
	}
	file, err := os.Open(path)
	if err != nil {
		return unknown
	}
	data, err := io.ReadAll(io.LimitReader(file, *left+1))
	file.Close()
	if err != nil {
		return unknown
	}
	*left -= int64(len(data))
	if *left < 0 {
		return refuseLand(LandTooLarge, "this land's attribute files are larger than it reads")
	}
	fmt.Fprintf(sum, "%d:", len(data))
	sum.Write(data)
	return nil
}

// landFilterDrivers names the filter drivers that have a program to run. With
// none, no attribute can make git run a filter, so a land asks git nothing about
// attributes: an older git without those queries still lands, and a driver added
// after the plan changes the configuration digest the plan already binds.
func landFilterDrivers(config landConfigFacts) map[string]bool {
	drivers := map[string]bool{}
	for key, values := range config.values {
		if !strings.HasPrefix(key, "filter.") {
			continue
		}
		dot := strings.LastIndexByte(key, '.')
		if dot < len("filter.") {
			continue
		}
		switch key[dot+1:] {
		case "clean", "smudge", "process":
			// Subsection names (the driver) are case-sensitive in git config.
			if len(values) > 0 && values[len(values)-1] != "" {
				drivers[key[len("filter."):dot]] = true
			}
		}
	}
	return drivers
}

//go:build !windows

// Built-in formatter registry
package format

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

type Context struct {
	Directory string
	Worktree  string
}

type EnabledFunc func(context.Context, Context) ([]string, bool, error)

type Info struct {
	Key         string
	Name        string
	Environment map[string]string
	Extensions  []string
	Enabled     EnabledFunc
}

// Dependencies isolates PATH, npm, filesystem, flags, and process probes.
type Dependencies struct {
	Which             func(string) (string, bool)
	NpmWhich          func(context.Context, string) (string, bool)
	ExperimentalOxfmt bool
}

func (d Dependencies) which(command string) (string, bool) {
	if d.Which == nil {
		return "", false
	}
	return d.Which(command)
}

func (d Dependencies) npmWhich(ctx context.Context, pkg string) (string, bool) {
	if d.NpmWhich == nil {
		return "", false
	}
	return d.NpmWhich(ctx, pkg)
}

// Builtins returns the built-in formatters. Every entry's Key, which is what
// a config override names, is also its Name, which is what it registers
// under, so an override reaches the formatter it names. The order is the
// order a file's candidates are collected in.
func Builtins(dependencies Dependencies) []Info {
	pathFormatter := func(key, name string, extensions []string, args ...string) Info {
		return Info{
			Key: key, Name: name, Extensions: extensions,
			Enabled: func(_ context.Context, _ Context) ([]string, bool, error) {
				match, ok := dependencies.which(name)
				if !ok {
					return nil, false, nil
				}
				command := []string{match}
				command = append(command, args...)
				command = append(command, "$FILE")
				return command, true, nil
			},
		}
	}
	bunEnvironment := map[string]string{"BUN_BE_BUN": "1"}
	out := []Info{}
	out = append(out, pathFormatter("gofmt", "gofmt", []string{".go"}, "-w"))
	out = append(out, pathFormatter("mix", "mix", []string{".ex", ".exs", ".eex", ".heex", ".leex", ".neex", ".sface"}, "format"))
	out = append(out, Info{
		Key: "prettier", Name: "prettier", Environment: bunEnvironment,
		Extensions: prettierExtensions(),
		Enabled: func(ctx context.Context, instance Context) ([]string, bool, error) {
			items := util.FindUp([]string{"package.json"}, instance.Directory, instance.Worktree)
			for _, item := range items {
				pkg, err := readPackageJSON(item)
				if err != nil {
					return nil, false, err
				}
				if dependencyPresent(pkg, "prettier") {
					if bin, ok := dependencies.npmWhich(ctx, "prettier"); ok {
						return []string{bin, "--write", "$FILE"}, true, nil
					}
				}
			}
			return nil, false, nil
		},
	})
	out = append(out, Info{
		Key: "oxfmt", Name: "oxfmt", Environment: bunEnvironment,
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"},
		Enabled: func(ctx context.Context, instance Context) ([]string, bool, error) {
			if !dependencies.ExperimentalOxfmt {
				return nil, false, nil
			}
			items := util.FindUp([]string{"package.json"}, instance.Directory, instance.Worktree)
			for _, item := range items {
				pkg, err := readPackageJSON(item)
				if err != nil {
					return nil, false, err
				}
				if dependencyPresent(pkg, "oxfmt") {
					if bin, ok := dependencies.npmWhich(ctx, "oxfmt"); ok {
						return []string{bin, "$FILE"}, true, nil
					}
				}
			}
			return nil, false, nil
		},
	})
	out = append(out, Info{
		Key: "biome", Name: "biome", Environment: bunEnvironment,
		Extensions: prettierExtensions(),
		Enabled: func(ctx context.Context, instance Context) ([]string, bool, error) {
			for _, config := range []string{"biome.json", "biome.jsonc"} {
				if len(util.FindUp([]string{config}, instance.Directory, instance.Worktree)) > 0 {
					if bin, ok := dependencies.npmWhich(ctx, "@biomejs/biome"); ok {
						return []string{bin, "format", "--write", "$FILE"}, true, nil
					}
				}
			}
			return nil, false, nil
		},
	})
	out = append(out, pathFormatter("zig", "zig", []string{".zig", ".zon"}, "fmt"))
	out = append(out, Info{
		Key: "clang-format", Name: "clang-format",
		Extensions: []string{".c", ".cc", ".cpp", ".cxx", ".c++", ".h", ".hh", ".hpp", ".hxx", ".h++", ".ino", ".C", ".H"},
		Enabled: func(_ context.Context, instance Context) ([]string, bool, error) {
			if len(util.FindUp([]string{".clang-format"}, instance.Directory, instance.Worktree)) == 0 {
				return nil, false, nil
			}
			match, ok := dependencies.which("clang-format")
			if !ok {
				return nil, false, nil
			}
			return []string{match, "-i", "$FILE"}, true, nil
		},
	})
	out = append(out, pathFormatter("ktlint", "ktlint", []string{".kt", ".kts"}, "-F"))
	ruff := Info{
		Key: "ruff", Name: "ruff", Extensions: []string{".py", ".pyi"},
		Enabled: func(_ context.Context, instance Context) ([]string, bool, error) {
			if _, ok := dependencies.which("ruff"); !ok {
				return nil, false, nil
			}
			for _, config := range []string{"pyproject.toml", "ruff.toml", ".ruff.toml"} {
				found := util.FindUp([]string{config}, instance.Directory, instance.Worktree)
				if len(found) == 0 {
					continue
				}
				if config == "pyproject.toml" {
					content, err := util.ReadText(found[0])
					if err != nil {
						return nil, false, err
					}
					if strings.Contains(content, "[tool.ruff]") {
						return []string{"ruff", "format", "$FILE"}, true, nil
					}
				} else {
					return []string{"ruff", "format", "$FILE"}, true, nil
				}
			}
			for _, dependency := range []string{"requirements.txt", "pyproject.toml", "Pipfile"} {
				found := util.FindUp([]string{dependency}, instance.Directory, instance.Worktree)
				if len(found) == 0 {
					continue
				}
				content, err := util.ReadText(found[0])
				if err != nil {
					return nil, false, err
				}
				if strings.Contains(content, "ruff") {
					return []string{"ruff", "format", "$FILE"}, true, nil
				}
			}
			return nil, false, nil
		},
	}
	out = append(out, ruff)
	out = append(out, Info{
		Key: "air", Name: "air", Extensions: []string{".R"},
		Enabled: func(ctx context.Context, _ Context) ([]string, bool, error) {
			air, ok := dependencies.which("air")
			if !ok {
				return nil, false, nil
			}
			result, _ := util.TextProcess(ctx, []string{air, "--help"}, util.RunOptions{NoThrow: true})
			firstLine := strings.Split(result.Text, "\n")[0]
			if result.Code == 0 && strings.Contains(firstLine, "R language") && strings.Contains(firstLine, "formatter") {
				return []string{air, "format", "$FILE"}, true, nil
			}
			return nil, false, nil
		},
	})
	out = append(out, Info{
		Key: "uv", Name: "uv", Extensions: []string{".py", ".pyi"},
		Enabled: func(ctx context.Context, instance Context) ([]string, bool, error) {
			if _, enabled, err := ruff.Enabled(ctx, instance); err != nil || enabled {
				return nil, false, err
			}
			uv, ok := dependencies.which("uv")
			if !ok {
				return nil, false, nil
			}
			result, _ := util.RunProcess(ctx, []string{uv, "format", "--help"}, util.RunOptions{NoThrow: true})
			if result.Code == 0 {
				return []string{uv, "format", "--", "$FILE"}, true, nil
			}
			return nil, false, nil
		},
	})
	out = append(out, pathFormatter("rubocop", "rubocop", []string{".rb", ".rake", ".gemspec", ".ru"}, "--autocorrect"))
	out = append(out, pathFormatter("standardrb", "standardrb", []string{".rb", ".rake", ".gemspec", ".ru"}, "--fix"))
	out = append(out, pathFormatter("htmlbeautifier", "htmlbeautifier", []string{".erb", ".html.erb"}))
	out = append(out, pathFormatter("dart", "dart", []string{".dart"}, "format"))
	out = append(out, Info{
		Key: "ocamlformat", Name: "ocamlformat", Extensions: []string{".ml", ".mli"},
		Enabled: func(_ context.Context, instance Context) ([]string, bool, error) {
			if _, ok := dependencies.which("ocamlformat"); !ok {
				return nil, false, nil
			}
			if len(util.FindUp([]string{".ocamlformat"}, instance.Directory, instance.Worktree)) > 0 {
				return []string{"ocamlformat", "-i", "$FILE"}, true, nil
			}
			return nil, false, nil
		},
	})
	out = append(out, pathFormatter("terraform", "terraform", []string{".tf", ".tfvars"}, "fmt"))
	out = append(out, pathFormatter("latexindent", "latexindent", []string{".tex"}, "-w", "-s"))
	out = append(out, pathFormatter("gleam", "gleam", []string{".gleam"}, "format"))
	out = append(out, pathFormatter("shfmt", "shfmt", []string{".sh", ".bash"}, "-w"))
	out = append(out, pathFormatter("nixfmt", "nixfmt", []string{".nix"}))
	out = append(out, pathFormatter("rustfmt", "rustfmt", []string{".rs"}))
	out = append(out, Info{
		Key: "pint", Name: "pint", Extensions: []string{".php"},
		Enabled: func(_ context.Context, instance Context) ([]string, bool, error) {
			items := util.FindUp([]string{"composer.json"}, instance.Directory, instance.Worktree)
			for _, item := range items {
				data, err := os.ReadFile(item)
				if err != nil {
					return nil, false, err
				}
				var composer struct {
					Require    map[string]string `json:"require"`
					RequireDev map[string]string `json:"require-dev"`
				}
				if err := json.Unmarshal(data, &composer); err != nil {
					return nil, false, err
				}
				if composer.Require["laravel/pint"] != "" || composer.RequireDev["laravel/pint"] != "" {
					return []string{"./vendor/bin/pint", "$FILE"}, true, nil
				}
			}
			return nil, false, nil
		},
	})
	out = append(out, pathFormatter("ormolu", "ormolu", []string{".hs"}, "-i"))
	out = append(out, pathFormatter("cljfmt", "cljfmt", []string{".clj", ".cljs", ".cljc", ".edn"}, "fix", "--quiet"))
	out = append(out, pathFormatter("dfmt", "dfmt", []string{".d"}, "-i"))
	// Sorted by key so the registration order is deterministic.
	slices.SortFunc(out, func(left, right Info) int {
		return strings.Compare(left.Key, right.Key)
	})
	return out
}

func prettierExtensions() []string {
	return []string{
		".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts",
		".html", ".htm", ".css", ".scss", ".sass", ".less", ".vue", ".svelte",
		".json", ".jsonc", ".yaml", ".yml", ".toml", ".xml", ".md", ".mdx",
		".graphql", ".gql",
	}
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func readPackageJSON(path string) (packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageJSON{}, err
	}
	var pkg packageJSON
	err = json.Unmarshal(data, &pkg)
	return pkg, err
}

func dependencyPresent(pkg packageJSON, name string) bool {
	return pkg.Dependencies[name] != "" || pkg.DevDependencies[name] != ""
}

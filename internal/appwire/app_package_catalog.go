package appwire

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

const packageSuffix = ".codeaf-app"

type packagePresentation struct {
	Job          string   `json:"job"`
	Role         string   `json:"role,omitempty"`
	Featured     bool     `json:"featured,omitempty"`
	Screen       string   `json:"screen,omitempty"`
	Before       string   `json:"before,omitempty"`
	After        string   `json:"after,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	ReadyMinutes int      `json:"ready_minutes,omitempty"`
}

type catalogPackage struct {
	apppackage.Source
	packageParams
	packagePresentation
	InstalledApp string `json:"installed_app,omitempty"`
}

type packageCatalog struct {
	Packages []catalogPackage `json:"packages"`
}

func kitPackagesDir() string { return home.Join("kit", "apps") }

func init() {
	methods["apps.browse"] = appCall(func(s *store.Store, p appParams) (*packageCatalog, error) {
		var source *apppackage.Source
		if p.App != "" {
			d, err := s.Get(p.App)
			if err != nil {
				return nil, err
			}
			if d.Meta.Lineage != nil {
				source = d.Meta.Lineage.Source
			}
			if source == nil {
				return &packageCatalog{Packages: []catalogPackage{}}, nil
			}
		}
		personal := os.DirFS(kitPackagesDir())
		builtin, err := apppackage.CatalogFiles("firstparty")
		if err != nil {
			return nil, err
		}
		examples, err := apppackage.CatalogFiles("examples")
		if err != nil {
			return nil, err
		}
		packages := map[[2]string]catalogPackage{}
		for _, catalog := range []struct {
			files     fs.FS
			directory string
		}{{personal, ""}, {builtin, "firstparty"}, {examples, "examples"}} {
			embedded := catalog.directory != ""
			entries, err := fs.ReadDir(catalog.files, ".")
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				name := entry.Name()
				if !entry.Type().IsRegular() || !strings.HasSuffix(name, packageSuffix) {
					continue
				}
				p := packageParams{Path: filepath.Join(kitPackagesDir(), name)}
				if embedded {
					p = packageParams{Builtin: name}
					if catalog.directory == "examples" {
						p.Builtin = "examples/" + name
					}
				}
				a, err := selectedPackage(p)
				if err != nil || (source != nil && !apppackage.Newer(a.Manifest.Source, *source)) {
					continue
				}
				key := [2]string{a.Manifest.Publisher, a.Manifest.ID}
				old, ok := packages[key]
				// An embedded release wins over a file with the same publisher and ID.
				if ok && ((!embedded && old.Builtin != "") ||
					(embedded == (old.Builtin != "") && !apppackage.Newer(a.Manifest.Source, old.Source))) {
					continue
				}
				entry := catalogPackage{Source: a.Manifest.Source, packageParams: p, packagePresentation: packageDisplay(catalog.files, name, a)}
				// Bundled apps may already be published under their stable ID.
				if source == nil && catalog.directory == "firstparty" {
					if app, err := s.Get(entry.ID); err == nil && !app.Meta.Archived && app.Meta.Lineage != nil {
						if installed := app.Meta.Lineage.Source; installed != nil && installed.ID == entry.ID && installed.Publisher == entry.Publisher {
							entry.InstalledApp = app.Meta.ID
						}
					}
				}
				packages[key] = entry
			}
		}
		out := packageCatalog{Packages: make([]catalogPackage, 0, len(packages))}
		for _, p := range packages {
			out.Packages = append(out.Packages, p)
		}
		sort.Slice(out.Packages, func(i, j int) bool {
			a, b := out.Packages[i], out.Packages[j]
			if (a.Builtin != "") != (b.Builtin != "") {
				return a.Builtin != ""
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if a.Publisher != b.Publisher {
				return a.Publisher < b.Publisher
			}
			return a.ID < b.ID
		})
		return &out, nil
	})
}

func packageDisplay(files fs.FS, name string, a apppackage.Archive) packagePresentation {
	p := packagePresentation{}
	if raw := readPackageCompanion(files, name+".json", 4096); len(raw) != 0 {
		_ = json.Unmarshal(raw, &p)
	}
	switch p.Role {
	case "founders", "sales", "engineering", "finance-legal", "everyone":
	default:
		p.Role = "everyone"
	}
	if p.Job == "" {
		p.Job = packageJobLine(files, name, a)
	}
	// Declared connectors are shown when a package has no authored catalog card.
	if len(p.Sources) == 0 {
		for id := range maps.Keys(a.Manifest.Permissions.Connectors) {
			p.Sources = append(p.Sources, id)
		}
		sort.Strings(p.Sources)
	}
	return p
}

func packageJobLine(files fs.FS, name string, a apppackage.Archive) string {
	line := string(readPackageCompanion(files, name+".txt", 2048))
	if strings.TrimSpace(line) == "" {
		if plan, err := schema.ParsePlan(a.Files[schema.PlanFile]); err == nil {
			line = plan.Ask
		}
	}
	line, _, _ = strings.Cut(strings.TrimSpace(line), "\n")
	line = strings.Join(strings.Fields(line), " ")
	if runes := []rune(line); len(runes) > 160 {
		return string(runes[:159]) + "…"
	}
	return line
}

func readPackageCompanion(files fs.FS, name string, limit int) []byte {
	info, err := fs.Stat(files, name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil
	}
	f, err := files.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil
	}
	return data
}

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

const (
	packageSuffix     = ".codeaf-app"
	minExampleRecords = 30
)

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
		installed := map[[2]string]store.Summary{}
		if p.App == "" {
			apps, err := s.List()
			if err != nil {
				return nil, err
			}
			for _, candidate := range apps {
				if candidate.Status != "active" {
					continue
				}
				app, err := s.Get(candidate.ID)
				if err != nil || app.Meta.Lineage == nil || app.Meta.Lineage.Source == nil {
					continue
				}
				source := app.Meta.Lineage.Source
				key := [2]string{source.Publisher, source.ID}
				if previous, ok := installed[key]; !ok || candidate.UpdatedAt.After(previous.UpdatedAt) {
					installed[key] = candidate
				}
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
				entry.InstalledApp = installed[key].ID
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

// A curated release earns its place in Browse for one exact archive.
func exampleAccepted(files fs.FS, name string, a apppackage.Archive) bool {
	var evidence struct {
		Status string `json:"acceptance"`
		Hash   string `json:"accepted_hash"`
	}
	if json.Unmarshal(readPackageCompanion(files, name+".json", 4096), &evidence) != nil ||
		(evidence.Status != "passed" && evidence.Status != "golden") ||
		evidence.Hash == "" || evidence.Hash != a.Manifest.Hash {
		return false
	}
	var sample schema.Sample
	if json.Unmarshal(a.Files[schema.SampleFile], &sample) != nil {
		return false
	}
	count := 0
	for _, rows := range sample.Records {
		count += len(rows)
	}
	return count >= minExampleRecords
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

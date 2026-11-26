package appwire

import (
	"bytes"
	"errors"
	"image/png"
	"io"
	"io/fs"
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
	maxPreviewBytes   = 256 << 10
	maxBrowsePreviews = 2 << 20
)

type catalogPackage struct {
	apppackage.Source
	packageParams
	Job     string `json:"job"`
	Preview []byte `json:"preview,omitempty"`
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
		builtin, err := apppackage.CatalogFiles()
		if err != nil {
			return nil, err
		}
		packages := map[[2]string]catalogPackage{}
		for _, catalog := range []struct {
			files   fs.FS
			builtin bool
		}{{personal, false}, {builtin, true}} {
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
				if catalog.builtin {
					p = packageParams{Builtin: name}
				}
				a, err := selectedPackage(p)
				if err != nil || (source != nil && !apppackage.Newer(a.Manifest.Source, *source)) {
					continue
				}
				key := [2]string{a.Manifest.Publisher, a.Manifest.ID}
				old, ok := packages[key]
				// A personal archive cannot replace an embedded publisher's entry.
				if ok && (catalog.builtin == (old.Builtin != "")) && !apppackage.Newer(a.Manifest.Source, old.Source) {
					continue
				}
				packages[key] = catalogPackage{Source: a.Manifest.Source, packageParams: p, Job: packageJobLine(catalog.files, name, a)}
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
		remaining := maxBrowsePreviews
		for i := range out.Packages {
			p := &out.Packages[i]
			files, name := personal, filepath.Base(p.Path)
			if p.Builtin != "" {
				files, name = builtin, p.Builtin
			}
			image := readPackageCompanion(files, name+".png", min(maxPreviewBytes, remaining))
			if validPreview(image) {
				p.Preview = image
				remaining -= len(image)
			}
		}
		return &out, nil
	})
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

func validPreview(image []byte) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(image))
	return err == nil && cfg.Width <= 2048 && cfg.Height <= 2048
}

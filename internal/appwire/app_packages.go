package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

type packParams struct {
	App  string `json:"app"`
	Path string `json:"path"`
	apppackage.PackOptions
}

type packageParams struct {
	Path    string `json:"path,omitempty"`
	Builtin string `json:"builtin,omitempty"`
	Hash    string `json:"expected_hash,omitempty"`
}

type updatePackageParams struct {
	App string `json:"app"`
	packageParams
}

type resolvePackageParams struct {
	Draft    string            `json:"draft"`
	Proposal string            `json:"proposal_id"`
	Choices  map[string]string `json:"choices"`
}

type packResult struct {
	Path     string              `json:"path"`
	Manifest apppackage.Manifest `json:"manifest"`
}

type lineageResult struct {
	Lineage *store.Lineage `json:"lineage,omitempty"`
}

func init() {
	methods["apps.pack"] = buildCall(func(b *builder.Jobs, p packParams) (*packResult, error) {
		if p.Path != "" && !filepath.IsAbs(p.Path) {
			return nil, Errorf(CodeBadParams, "Choose an absolute path for the exported app package.")
		}
		raw, err := b.Pack(p.App, p.PackOptions)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrBadInput) {
			return nil, err
		}
		if err != nil {
			return nil, Errorf(CodeBadParams, "%s", err)
		}
		var result packResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		if p.Path == "" {
			if err := os.MkdirAll(kitPackagesDir(), 0700); err != nil {
				return nil, Errorf(CodeEngine, "The Kit package folder could not be opened.")
			}
			p.Path = filepath.Join(kitPackagesDir(), strings.TrimPrefix(result.Manifest.Hash, "sha256:")+packageSuffix)
			if existing, err := readPackage(p.Path); err == nil && existing.Manifest.Hash == result.Manifest.Hash {
				result.Path = p.Path
				return &result, nil
			}
		}
		if err := writePackage(p.Path, raw); err != nil {
			if errors.Is(err, os.ErrExist) {
				return nil, Errorf(CodeConflict, "A file already exists at the package path. Choose a new filename.")
			}
			return nil, Errorf(CodeBadParams, "The app package could not be written to the selected path.")
		}
		result.Path = p.Path
		return &result, nil
	})
	methods["apps.install"] = buildCall(func(b *builder.Jobs, p packageParams) (*builder.Proposal, error) {
		a, err := selectedPackage(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := packageContext()
		defer cancel()
		card, err := b.Install(ctx, a)
		return &card, packageError(err)
	})
	methods["apps.update"] = buildCall(func(b *builder.Jobs, p updatePackageParams) (*builder.Proposal, error) {
		a, err := selectedPackage(p.packageParams)
		if err != nil {
			return nil, err
		}
		ctx, cancel := packageContext()
		defer cancel()
		card, err := b.Update(ctx, p.App, a)
		return &card, packageError(err)
	})
	methods["apps.preview_package"] = buildCall(func(b *builder.Jobs, p packageParams) (*builder.Preview, error) {
		a, err := selectedPackage(p)
		if err != nil {
			return nil, err
		}
		preview, err := b.PreviewPackage(schema.Definition{Files: a.Files})
		return &preview, packageError(err)
	})
	methods["apps.close_package_preview"] = buildCall(func(b *builder.Jobs, p draftParams) (*Empty, error) {
		b.ClosePackagePreview(p.Draft)
		return &Empty{}, nil
	})
	methods["apps.start_package"] = buildCall(func(b *builder.Jobs, p packageParams) (*draftParams, error) {
		a, err := selectedPackage(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := packageContext()
		defer cancel()
		id, err := b.StartPackage(ctx, a)
		return &draftParams{Draft: id}, packageError(err)
	})
	methods["apps.fork"] = buildCall(func(b *builder.Jobs, p updatePackageParams) (*builder.Proposal, error) {
		ctx, cancel := packageContext()
		defer cancel()
		if p.App == "" {
			a, err := selectedPackage(p.packageParams)
			if err != nil {
				return nil, err
			}
			card, err := b.ForkPackage(ctx, a)
			return &card, packageError(err)
		}
		if p.Path != "" || p.Builtin != "" {
			return nil, Errorf(CodeBadParams, "Choose either an installed app or an app package to fork.")
		}
		card, err := b.Fork(ctx, p.App)
		return &card, packageError(err)
	})
	methods["apps.resolve_package"] = buildCall(func(b *builder.Jobs, p resolvePackageParams) (*builder.Proposal, error) {
		ctx, cancel := packageContext()
		defer cancel()
		card, err := b.ResolvePackage(ctx, p.Draft, p.Proposal, p.Choices)
		return &card, packageError(err)
	})
	methods["apps.lineage"] = appCall(func(s *store.Store, p appParams) (*lineageResult, error) {
		d, err := s.Get(p.App)
		return &lineageResult{Lineage: d.Meta.Lineage}, err
	})
}

func packageContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 295*time.Second)
}

func packageError(err error) error {
	var findings *apppackage.Findings
	if errors.As(err, &findings) {
		return &Error{Code: CodeBadParams, Message: findings.Error(), Detail: findings.Report}
	}
	if errors.Is(err, store.ErrBadInput) {
		return Errorf(CodeBadParams, "%s", err)
	}
	return err
}

func selectedPackage(p packageParams) (apppackage.Archive, error) {
	var a apppackage.Archive
	var err error
	if p.Builtin == "" {
		a, err = readPackage(p.Path)
	} else {
		name, example := strings.CutPrefix(p.Builtin, "examples/")
		directory := "firstparty"
		if example {
			directory = "examples"
		}
		if p.Path != "" || filepath.Base(name) != name || !strings.HasSuffix(name, packageSuffix) {
			return a, Errorf(CodeBadParams, "Choose one app package.")
		}
		catalog, fsErr := apppackage.CatalogFiles(directory)
		if fsErr != nil {
			return a, fsErr
		}
		f, openErr := catalog.Open(name)
		if openErr != nil {
			return a, Errorf(CodeBadParams, "This Kit package isn't available.")
		}
		defer f.Close()
		a, err = apppackage.Read(f)
		if err != nil {
			err = Errorf(CodeBadParams, "%s", err)
		}
	}
	if err == nil && p.Hash != "" && a.Manifest.Hash != p.Hash {
		return apppackage.Archive{}, Errorf(CodeConflict, "This package changed. Refresh Browse and try again.")
	}
	return a, err
}

func readPackage(path string) (apppackage.Archive, error) {
	if !filepath.IsAbs(path) {
		return apppackage.Archive{}, Errorf(CodeBadParams, "Choose an absolute path to an app package.")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return apppackage.Archive{}, Errorf(CodeBadParams, "Choose a readable app package file.")
	}
	f, err := os.Open(path)
	if err != nil {
		return apppackage.Archive{}, Errorf(CodeBadParams, "The app package file could not be opened.")
	}
	defer f.Close()
	a, err := apppackage.Read(f)
	if err != nil {
		return apppackage.Archive{}, Errorf(CodeBadParams, "%s", err)
	}
	return a, nil
}

// Write in the destination directory and link only the finished file, so a
// failed export leaves no partial archive and never replaces another file.
func writePackage(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".app-package-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}

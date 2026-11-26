package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

type packParams struct {
	App  string `json:"app"`
	Path string `json:"path"`
	apppackage.PackOptions
}

type packageParams struct {
	Path    string `json:"path"`
	Package string `json:"package"`
}

type updatePackageParams struct {
	App     string `json:"app"`
	Path    string `json:"path"`
	Package string `json:"package"`
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
		if !filepath.IsAbs(p.Path) {
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
		a, err := packageFrom(p.Path, p.Package)
		if err != nil {
			return nil, err
		}
		ctx, cancel := packageContext()
		defer cancel()
		card, err := b.Install(ctx, a)
		return &card, packageError(err)
	})
	methods["apps.update"] = buildCall(func(b *builder.Jobs, p updatePackageParams) (*builder.Proposal, error) {
		a, err := packageFrom(p.Path, p.Package)
		if err != nil {
			return nil, err
		}
		ctx, cancel := packageContext()
		defer cancel()
		card, err := b.Update(ctx, p.App, a)
		return &card, packageError(err)
	})
	methods["apps.fork"] = buildCall(func(b *builder.Jobs, p appParams) (*builder.Proposal, error) {
		ctx, cancel := packageContext()
		defer cancel()
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

func packageFrom(path, id string) (apppackage.Archive, error) {
	if id == "" {
		return readPackage(path)
	}
	if path != "" {
		return apppackage.Archive{}, Errorf(CodeBadParams, "Choose a Kit template or an app package file.")
	}
	archives, err := apppackage.Builtins()
	if err != nil {
		return apppackage.Archive{}, err
	}
	for _, archive := range archives {
		if archive.Manifest.ID == id {
			return archive, nil
		}
	}
	return apppackage.Archive{}, Errorf(CodeBadParams, "This Kit template is unavailable.")
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

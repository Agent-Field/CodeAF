package builder

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type PackageCard struct {
	Manifest     apppackage.Manifest           `json:"manifest"`
	Bindings     map[string]apppackage.Binding `json:"bindings"`
	ChangedFiles []string                      `json:"changed_files,omitempty"`
	Choices      map[string]string             `json:"choices,omitempty"`
	Conflicts    []apppackage.Conflict         `json:"conflicts,omitempty"`
	Permissions  schema.Capabilities           `json:"permissions"`
	Budget       *schema.Budget                `json:"budget,omitempty"`
}

type packageEdit struct {
	Publication store.PackagePublication
	Base        map[string]string
	Choices     map[string]string
}

func (s *Jobs) Pack(app string, options apppackage.PackOptions) ([]byte, error) {
	live, err := s.opts.Store.Get(app)
	if err != nil {
		return nil, err
	}
	catalog, err := s.catalog(app)
	if err != nil {
		return nil, err
	}
	if options.Version == "" {
		options.Version = fmt.Sprintf("%d.0.0", live.Meta.Version)
	}
	raw, err := apppackage.Pack(live.Files(), catalog.Types, options)
	if err != nil {
		return nil, err
	}
	archive, err := apppackage.Read(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if err := checkPackageInEmptyProfile(archive); err != nil {
		return nil, err
	}
	return raw, nil
}

// Check the receiving definition and sample store without the exporting
// profile's types or records. Exporting from a populated profile can hide
// dependencies that would otherwise fail the first time someone installs it.
func checkPackageInEmptyProfile(archive apppackage.Archive) error {
	files, _, err := archive.Bind(nil, "", "")
	if err != nil {
		return err
	}
	def := definition(files)
	if report := check.Definition(def, nil); !report.OK() {
		return &apppackage.Findings{Report: report}
	}
	rows, err := check.Sample(def)
	if err != nil {
		return &apppackage.Findings{Report: schema.Report{Problems: []schema.Problem{
			{File: schema.SampleFile, Code: "sample_invalid", Message: err.Error()},
		}}}
	}
	root, err := os.MkdirTemp("", "codeaf-pack-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	place := data.NewPlace(root, data.Options{NoRefresh: true})
	defer place.Close()
	if err := place.SeedSample("package", def.Files[schema.AppFile], rows); err != nil {
		return &apppackage.Findings{Report: schema.Report{Problems: []schema.Problem{
			{File: schema.SampleFile, Code: "package_clean_install", Message: "The package could not load in an empty profile. " + err.Error()},
		}}}
	}
	return nil
}

// Install stages a checked copy. Only Apply can publish the definition and
// its grants. In particular, package sample rows stay in preview storage.
func (s *Jobs) Install(ctx context.Context, archive apppackage.Archive) (Proposal, error) {
	return s.stagePackage(ctx, "", archive, nil)
}

func (s *Jobs) Fork(ctx context.Context, app string) (Proposal, error) {
	live, err := s.opts.Store.Get(app)
	if err != nil {
		return Proposal{}, err
	}
	catalog, err := s.catalog(app)
	if err != nil {
		return Proposal{}, err
	}
	raw, err := apppackage.Pack(live.Files(), catalog.Types, apppackage.PackOptions{})
	if err != nil {
		return Proposal{}, err
	}
	archive, err := apppackage.Read(strings.NewReader(string(raw)))
	if err != nil {
		return Proposal{}, err
	}
	return s.stagePackage(ctx, "", archive, &store.ForkOrigin{App: app, Version: live.Meta.Version, Source: sourceOf(live.Meta)})
}

// ForkPackage records the packaged definition as the fork's parent without following its updates.
func (s *Jobs) ForkPackage(ctx context.Context, archive apppackage.Archive) (Proposal, error) {
	source := archive.Manifest.Source
	return s.stagePackage(ctx, "", archive, &store.ForkOrigin{Source: &source})
}

// StartPackage seeds an ordinary Builder job, so subsequent requests can edit its files.
func (s *Jobs) StartPackage(ctx context.Context, archive apppackage.Archive) (id string, err error) {
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	catalog, err := s.catalog("")
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	j, err := s.create("")
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(filepath.Join(s.root, j.Draft))
		}
	}()
	files, _, err := archive.Bind(catalog.Types, j.Draft, "")
	if err != nil {
		return "", err
	}
	report, err := s.checkPackagedFiles(ctx, "", files)
	if err != nil {
		return "", err
	}
	j.Working = snapshot{Files: files, Passed: true, CheckComplete: true, Look: report.Look}
	j.Dirty, j.Editable = true, true
	j.State.State = Ready
	j.LastRequest = "Start from " + archive.Manifest.Name
	j.acceptKeptUses()
	j.describe(catalog)
	if err := j.checkpoint(j.LastRequest); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.save(j, Event{Stage: Ready, Line: j.LastRequest}); err != nil {
		return "", err
	}
	return j.Draft, nil
}

func (s *Jobs) Update(ctx context.Context, app string, archive apppackage.Archive) (Proposal, error) {
	if !cell.ValidID(app) {
		return Proposal{}, store.ErrBadInput
	}
	return s.stagePackage(ctx, app, archive, nil)
}

func sourceOf(meta store.Meta) *apppackage.Source {
	if meta.Lineage != nil {
		return meta.Lineage.Source
	}
	return nil
}

func (s *Jobs) stagePackage(ctx context.Context, app string, archive apppackage.Archive, fork *store.ForkOrigin) (Proposal, error) {
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	j, created, err := s.packageJob(app)
	if err != nil {
		return Proposal{}, err
	}
	committed := false
	defer func() {
		if created && !committed {
			_ = os.RemoveAll(filepath.Join(s.root, j.Draft))
		}
	}()

	var edit packageEdit
	card := Proposal{App: app, Draft: j.Draft, Kind: "big", Source: "package_install", BaseVersion: j.Base,
		Package: &PackageCard{Manifest: archive.Manifest}}
	card.ID, err = cell.NewID(time.Now())
	if err != nil {
		return Proposal{}, err
	}
	source := archive.Manifest.Source
	if fork == nil {
		edit.Publication.Lineage.Source = &source
	} else {
		edit.Publication.Lineage.ForkedFrom = fork
		card.Source = "package_fork"
	}

	var live store.Definition
	if app != "" {
		live, err = s.opts.Store.Get(app)
		if err != nil {
			return Proposal{}, err
		}
		if live.Meta.Version != j.Base {
			return Proposal{}, store.ErrConflict
		}
		before := sourceOf(live.Meta)
		if before == nil || !apppackage.Newer(source, *before) {
			return Proposal{}, &PlanFailure{Message: "Choose a newer version of this app's original package from the same publisher."}
		}
		edit.Base, err = s.opts.Store.PackageBase(app, live.Meta.Lineage.PackageBaseVersion)
		if err != nil {
			return Proposal{}, err
		}
		edit.Publication.Lineage.ForkedFrom = live.Meta.Lineage.ForkedFrom
		card.Source = "package_update"
	}

	catalog, err := s.catalog(app)
	if err != nil {
		return Proposal{}, err
	}
	target := app
	if target == "" {
		target = j.Draft
	}
	var forkFrom string
	if parent := edit.Publication.Lineage.ForkedFrom; parent != nil {
		forkFrom = parent.App
	}
	upstream, bindings, err := archive.Bind(catalog.Types, target, forkFrom)
	if err != nil {
		return Proposal{}, err
	}
	card.Package.Bindings = bindings
	files := upstream
	if edit.Publication.Lineage.Source != nil {
		edit.Publication.Baseline = upstream
	}
	if fork != nil && fork.Source != nil && fork.Source.Publisher == apppackage.BuiltinPublisher {
		parent, err := s.opts.Store.Get(fork.App)
		if err != nil {
			return Proposal{}, err
		}
		if parent.Meta.Version != fork.Version || parent.Meta.Lineage == nil {
			return Proposal{}, store.ErrConflict
		}
		base, err := s.opts.Store.PackageBase(fork.App, parent.Meta.Lineage.PackageBaseVersion)
		if err != nil {
			return Proposal{}, err
		}
		// Rebind the publisher's baseline, not the locally edited parent.
		// That difference is what a later update must preserve on the fork.
		raw, err := apppackage.Pack(base, catalog.Types, apppackage.PackOptions{})
		if err != nil {
			return Proposal{}, err
		}
		publisher, err := apppackage.Read(strings.NewReader(string(raw)))
		if err != nil {
			return Proposal{}, err
		}
		edit.Publication.Lineage.Source = fork.Source
		edit.Publication.Baseline, _, err = publisher.Bind(catalog.Types, target, forkFrom)
		if err != nil {
			return Proposal{}, err
		}
		// Show the signed upstream even though the fork itself is packed locally.
		source = *fork.Source
	}
	if app != "" {
		files, card.Package.Conflicts, err = apppackage.Merge(edit.Base, live.Files(), upstream, nil)
		if err != nil {
			return Proposal{}, err
		}
	}
	card.SummaryLines = []string{packageSummary(card.Source, source)}
	err = s.savePackageCandidate(ctx, j, card, edit, live.Files(), files)
	committed = err == nil
	if err != nil {
		return Proposal{}, err
	}
	return *j.Proposal, nil
}

func packageSummary(source string, pkg apppackage.Source) string {
	label := "Install"
	switch source {
	case "package_update":
		label = "Update"
	case "package_fork":
		label = "Fork"
	}
	return fmt.Sprintf("%s %s %s from %s.", label, pkg.Name, pkg.Version, pkg.Publisher)
}

func (s *Jobs) packageJob(app string) (*job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if app != "" {
		jobs, err := s.jobs()
		if err != nil {
			return nil, false, err
		}
		for _, j := range jobs {
			if j.App != app {
				continue
			}
			if j.Dirty || j.Publishing || len(j.Queue) != 0 || j.Paused != nil {
				return nil, false, store.ErrConflict
			}
			live, err := s.opts.Store.Get(app)
			if err != nil {
				return nil, false, err
			}
			j.Base = live.Meta.Version
			return j, false, nil
		}
	}
	j, err := s.create(app)
	return j, true, err
}

// ResolvePackage remerges the original three inputs. All conflicts must have
// a choice before Check and Apply. Keeping local preserves the next upstream
// baseline, so a publisher's already-declined change is not proposed again.
func (s *Jobs) ResolvePackage(ctx context.Context, draft, proposal string, choices map[string]string) (Proposal, error) {
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	s.mu.Lock()
	j, err := s.load(draft)
	s.mu.Unlock()
	if err != nil {
		return Proposal{}, err
	}
	if j.Publishing || j.Package == nil || j.Proposal == nil || j.Proposal.ID != proposal ||
		j.Proposal.Source != "package_update" || len(j.Proposal.Package.Conflicts) == 0 {
		return Proposal{}, store.ErrConflict
	}
	live, err := s.opts.Store.Get(j.App)
	if err != nil {
		return Proposal{}, err
	}
	if live.Meta.Version != j.Base {
		return Proposal{}, store.ErrConflict
	}
	edit := *j.Package
	local := live.Files()
	files, conflicts, err := apppackage.Merge(edit.Base, local, edit.Publication.Baseline, choices)
	if err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", store.ErrBadInput, err)
	}
	if len(choices) != len(conflicts) {
		return Proposal{}, fmt.Errorf("%w: choose a value for every conflict", store.ErrBadInput)
	}
	edit.Choices = maps.Clone(choices)
	if err := s.savePackageCandidate(ctx, j, *j.Proposal, edit, local, files); err != nil {
		return Proposal{}, err
	}
	return *j.Proposal, nil
}

func (s *Jobs) savePackageCandidate(ctx context.Context, j *job, card Proposal, edit packageEdit, local, files map[string]string) error {
	candidate, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil {
		return err
	}
	card.Package.Permissions, card.Package.Budget = candidate.Capabilities, candidate.Budget
	card.Package.ChangedFiles = changedFiles(local, files)
	card.Package.Choices = edit.Choices
	checked := len(card.Package.Conflicts) == len(edit.Choices)
	var report schema.Report
	if checked {
		report, err = s.checkPackagedFiles(ctx, j.App, files)
		if err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load(j.Draft)
	if err == nil && current.Revision != j.Revision {
		return store.ErrConflict
	}
	if err != nil && (j.Revision != 0 || err != store.ErrNotFound) {
		return err
	}
	if j.Revision == 0 && j.App != "" {
		jobs, err := s.jobs()
		if err != nil {
			return err
		}
		if slices.ContainsFunc(jobs, func(other *job) bool { return other.App == j.App }) {
			return store.ErrConflict
		}
	}
	j.Working = snapshot{Files: files, Passed: checked, CheckComplete: checked, Look: report.Look}
	j.Dirty, j.Editable, j.Package, j.Proposal = true, true, &edit, &card
	j.Name, j.LastRequest, j.Screens = candidate.Name, card.SummaryLines[0], screens(files)
	j.State.State, j.Problem = NeedsYou, "Choose how to combine the package changes."
	if checked {
		j.State.State, j.Problem = Ready, ""
		if err := j.checkpoint(card.SummaryLines[0]); err != nil {
			return err
		}
		card.Checkpoint = j.Checkpoints[len(j.Checkpoints)-1].ID
		if j.App != "" && len(j.Screens) == 1 {
			card.Screen = j.Screens[0]
		}
	}
	if err := s.save(j, Event{Stage: j.State.State, Line: card.SummaryLines[0]}); err != nil {
		return err
	}
	if s.opts.Proposed != nil {
		s.opts.Proposed(card)
	}
	return nil
}

func changedFiles(local, files map[string]string) []string {
	var changed []string
	for name, raw := range files {
		if previous, ok := local[name]; !ok || previous != raw {
			changed = append(changed, name)
		}
	}
	for name := range local {
		if _, ok := files[name]; !ok {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)
	return changed
}

func (s *Jobs) checkPackagedFiles(ctx context.Context, app string, files map[string]string) (schema.Report, error) {
	catalog, err := s.catalog(app)
	if err != nil {
		return schema.Report{}, err
	}
	def := definition(files)
	check := s.opts.CheckPackage
	if check == nil {
		check = s.opts.Check
	}
	if check == nil {
		return checkedPackageReport(schema.Validate(def, catalog.Types))
	}
	// Check and its renderer can call Preview/Screen while the candidate is
	// still detached from the durable job. The scope holds only sample data.
	id, err := cell.NewID(time.Now())
	if err != nil {
		return schema.Report{}, err
	}
	dir := filepath.Join(s.root, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return schema.Report{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "preview"), nil, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return schema.Report{}, err
	}
	s.mu.Lock()
	s.scopes[id] = previewScope{files: files}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removeScope(id)
	}()
	report := check(ctx, id, def, catalog.Types)
	if err := ctx.Err(); err != nil {
		return schema.Report{}, err
	}
	return checkedPackageReport(report)
}

func checkedPackageReport(report schema.Report) (schema.Report, error) {
	if !report.OK() {
		return report, &apppackage.Findings{Report: report}
	}
	return report, nil
}

func (s *Jobs) publishPackage(j *job) (store.Meta, error) {
	return s.opts.Store.PublishPackageBuild(j.Draft, j.App, j.Base, definition(j.Working.Files), j.LastRequest, j.Package.Publication)
}

func sameLineage(live, proposed *store.Lineage) bool {
	return live != nil && reflect.DeepEqual(live.Source, proposed.Source) && reflect.DeepEqual(live.ForkedFrom, proposed.ForkedFrom)
}

package tui3_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui3"
)

// adapter is a Folders implementation outside package tui3. cmd/codeaf's
// wiring adapter has to be able to name these types; unexported DTOs made
// that compile-impossible (n-ux5g).
type adapter struct{}

func (adapter) RootSnapshot(context.Context) (tui3.FolderRoot, error) {
	return tui3.FolderRoot{}, nil
}
func (adapter) FolderSnapshot(context.Context, string) (tui3.FolderView, []tui3.FolderPlacement, error) {
	return tui3.FolderView{}, nil, nil
}
func (adapter) CreateFolder(context.Context, string) (tui3.FolderView, error) {
	return tui3.FolderView{}, nil
}
func (adapter) RenameFolder(context.Context, string, string) error    { return nil }
func (adapter) AddPlacement(context.Context, string, string) error    { return nil }
func (adapter) RemovePlacement(context.Context, string, string) error { return nil }
func (adapter) MovePlacement(context.Context, string, string, string) error {
	return nil
}
func (adapter) WhyHere(context.Context, string, string) (tui3.FolderWhy, error) {
	return tui3.FolderWhy{}, nil
}

var _ tui3.Folders = adapter{}

func TestFoldersInterfaceIsImplementableOutsideThePackage(t *testing.T) {
	var folders tui3.Folders = adapter{}
	root, err := folders.RootSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if root.Folders != nil {
		t.Fatalf("empty adapter RootSnapshot returned folders: %+v", root.Folders)
	}
	view, members, err := folders.FolderSnapshot(context.Background(), "col-billing")
	if err != nil || members != nil || view.ID != "" {
		t.Fatalf("empty adapter FolderSnapshot: view=%+v members=%v err=%v", view, members, err)
	}
	why, err := folders.WhyHere(context.Background(), "col-billing", "aaaa")
	if err != nil || why.Origin != "" {
		t.Fatalf("empty adapter WhyHere: %+v err=%v", why, err)
	}
}

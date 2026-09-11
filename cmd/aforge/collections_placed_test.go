package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// A WHOLE FOLDER PRINTS EVERY PLACEMENT ONCE WHILE IT IS WRITTEN TO. Past one
// page the terminal's show is several reads, and a placement made or taken away
// between two of them used to move every row after it: from an offset, the
// next page repeated the last row printed (a placement landing below it) or
// skipped the next one (a placement taken away below it). Review of df9220d1f.
func TestAWholeFolderPrintsEveryPlacementOnceWhileItIsWrittenTo(t *testing.T) {
	for _, write := range []struct {
		name   string
		change func(context.Context, *workspace.Store, string) error
	}{
		{"a placement lands below the cursor", func(ctx context.Context, s *workspace.Store, folder string) error {
			return s.AddPlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: "item-0000"})
		}},
		{"a placement is taken away below the cursor", func(ctx context.Context, s *workspace.Store, folder string) error {
			return s.RemovePlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: "item-0001"})
		}},
	} {
		t.Run(write.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := workspace.Open(filepath.Join(t.TempDir(), "collections.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			folder, err := s.Create(ctx, "Big")
			if err != nil {
				t.Fatal(err)
			}
			// Every placement but the one the write touches is there before
			// and after it, so each must be printed exactly once.
			steady := map[string]bool{}
			for i := 1; i <= placedPage+2; i++ {
				id := fmt.Sprintf("item-%04d", i)
				if err := s.AddPlacement(ctx, folder.ID, workspace.Ref{Kind: workspace.StandingKind, ID: id}); err != nil {
					t.Fatal(err)
				}
				steady[id] = i != 1
			}
			reads := 0
			got, err := everythingPlaced(func(w workspace.PlacedWindow) ([]workspace.Ref, bool, error) {
				if reads++; reads == 2 {
					if err := write.change(ctx, s, folder.ID); err != nil {
						t.Fatal(err)
					}
				}
				return s.Placed(ctx, folder.ID, w)
			})
			if err != nil {
				t.Fatal(err)
			}
			if reads < 2 {
				t.Fatalf("%d placements were read in %d read; the write never fell between pages", placedPage+2, reads)
			}
			printed := map[string]int{}
			for _, ref := range got {
				printed[ref.ID]++
			}
			for id, n := range printed {
				if n > 1 {
					t.Errorf("%s was printed %d times", id, n)
				}
			}
			for id, kept := range steady {
				if kept && printed[id] != 1 {
					t.Errorf("%s was there throughout and printed %d times", id, printed[id])
				}
			}
		})
	}
}

package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStabEM4PlanLimitsAreAtomic(t *testing.T) {
	for _, name := range []string{"draft text", "milestones", "waves", "items", "encoded JSON"} {
		t.Run(name, func(t *testing.T) {
			s, _, now := testStore(t)
			ctx := context.Background()
			w := createStream(t, s, "Limits")
			milestones := []Milestone{}
			waves := []Wave{{N: 1, Items: []PlanItem{{Kind: "draft", DraftID: "d", Title: "Draft"}}}}
			switch name {
			case "draft text":
				waves[0].Items[0].Text = strings.Repeat("x", PlanDraftTextMax)
			case "milestones":
				for i := range PlanMilestonesMax {
					milestones = append(milestones, Milestone{ID: fmt.Sprintf("m%d", i), Title: "Milestone"})
				}
			case "waves":
				waves = []Wave{}
				for i := range PlanWavesMax {
					waves = append(waves, Wave{N: i + 1, Items: []PlanItem{}})
				}
			case "items":
				waves[0].Items = []PlanItem{}
				for i := range PlanItemsMax {
					waves[0].Items = append(waves[0].Items, PlanItem{Kind: "draft", DraftID: fmt.Sprintf("d%d", i), Title: "Draft"})
				}
			case "encoded JSON":
				waves[0].Items = []PlanItem{}
				for i := range 53 {
					waves[0].Items = append(waves[0].Items, PlanItem{Kind: "draft", DraftID: fmt.Sprintf("d%d", i), Title: "Draft", Text: strings.Repeat("x", PlanDraftTextMax)})
				}
				last := &waves[0].Items[52]
				last.Text = "x"
				m, err := json.Marshal(milestones)
				if err != nil {
					t.Fatal(err)
				}
				v, err := json.Marshal(waves)
				if err != nil {
					t.Fatal(err)
				}
				last.Text = strings.Repeat("x", 1+PlanEncodedMax-len(m)-len(v))
				if len(last.Text) > PlanDraftTextMax {
					t.Fatal("encoded fixture exceeds the draft text bound")
				}
			}
			before, c, err := s.SetPlan(ctx, w.ID, 0, milestones, waves, Attribution{})
			if err != nil || before.Version != 1 || len(c.Parts) == 0 || !reflect.DeepEqual(before.Milestones, milestones) || !reflect.DeepEqual(before.Waves, waves) {
				t.Fatalf("at bound: version=%d change=%+v: %v", before.Version, c, err)
			}
			row, err := s.Get(ctx, w.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "draft text":
				waves[0].Items[0].Text += "x"
			case "milestones":
				milestones = append(milestones, Milestone{ID: "extra", Title: "Extra"})
			case "waves":
				waves = append(waves, Wave{N: PlanWavesMax + 1, Items: []PlanItem{}})
			case "items":
				waves[0].Items = append(waves[0].Items, PlanItem{Kind: "draft", DraftID: "extra", Title: "Extra"})
			case "encoded JSON":
				waves[0].Items[52].Text += "x"
			}
			*now = now.Add(time.Minute)
			_, c, err = s.SetPlan(ctx, w.ID, before.Version, milestones, waves, Attribution{})
			if !errors.Is(err, ErrLimit) || len(c.Parts) != 0 || !strings.Contains(err.Error(), "at most") {
				t.Fatalf("over bound: change=%+v: %v", c, err)
			}
			after, err := s.Plan(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("refused plan changed: version=%d: %v", after.Version, err)
			}
			afterRow, err := s.Get(ctx, w.ID)
			if err != nil || !reflect.DeepEqual(row, afterRow) {
				t.Fatalf("refused plan touched the workstream: %v", err)
			}
		})
	}
}

func TestStabEM4DraftControlCharactersRemainInvalid(t *testing.T) {
	s, _, _ := testStore(t)
	w := createStream(t, s, "Control characters")
	for _, text := range []string{"bad\x00text", strings.Repeat("x", PlanDraftTextMax) + "\x00"} {
		_, c, err := s.SetPlan(context.Background(), w.ID, 0, nil, []Wave{{N: 1, Items: []PlanItem{{Kind: "draft", DraftID: "d", Title: "Draft", Text: text}}}}, Attribution{})
		if !errors.Is(err, ErrInvalid) || len(c.Parts) != 0 {
			t.Fatalf("control character in %d bytes: change=%+v: %v", len(text), c, err)
		}
	}
}

func TestStabEM4RestoreChecksSavedDocumentSize(t *testing.T) {
	s, db, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Restore")
	pages, err := s.Pages(ctx, w.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("Overview: %v", err)
	}
	before, err := s.Page(ctx, pages[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE workstream_page_versions SET doc=? WHERE page_id=? AND version=1`, `{"text":"`+strings.Repeat("x", DocMax)+`"}`, before.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, c, err := s.RestorePage(ctx, before.ID, 1, before.Version, Attribution{})
	if !errors.Is(err, ErrTooLarge) || len(c.Parts) != 0 {
		t.Fatalf("restore oversized saved document: change=%+v: %v", c, err)
	}
	after, err := s.Page(ctx, before.ID, false)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("refused restore changed the page: %v", err)
	}
}

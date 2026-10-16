package main

import (
	"testing"
)

func TestV3ModelsCarryOnlyPublishedPositiveListingTime(t *testing.T) {
	rows := v3Models(v3Rows{{ID: "dated", Created: 1760000000}, {ID: "unknown-2099-12-31"}, {ID: "negative", Created: -1}})
	if len(rows) != 3 || rows[0].ListedAt != 1760000000 || rows[1].ListedAt != 0 || rows[2].ListedAt != 0 {
		t.Fatalf("listing facts differ: %+v", rows)
	}
}

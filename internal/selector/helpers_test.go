package selector

// mkM builds a catalog row with the schema fields the selector reads.
func mkM(id, slug string, created int64, ctx int, tools, img bool, price float64) Model {
	m := Model{ID: id, CanonicalSlug: slug, Created: created,
		ContextLength: ctx, PromptPrice: price}
	if tools {
		m.ModalitiesIn = append(m.ModalitiesIn, "text")
	}
	if img {
		m.ModalitiesIn = append(m.ModalitiesIn, "image")
	}
	return m
}

// mkSnap builds a SharedSnapshot: scored posterior means (quality/ROLE/ID -> q),
// variances (quality/ROLE/ID -> sigma^2) and per-role residual sigmas.
func mkSnap(residual map[string]float64, scored, uncert map[string]float64) *SharedSnapshot {
	s := &SharedSnapshot{
		SchemaVersion: 1,
		Parameters:    map[string]float64{},
		Uncertainty:   map[string]float64{},
		Counts:        map[string]int{},
	}
	for k, v := range scored {
		s.Parameters[k] = v
	}
	for k, v := range uncert {
		s.Uncertainty[k] = v
	}
	for k := range scored {
		s.Counts[k] = 10
	}
	for role, sg := range residual {
		s.Parameters["quality_w/"+role+"/residual_sigma"] = sg
	}
	return s
}

func mkCat(ms ...Model) *ModelCatalog {
	c := &ModelCatalog{Models: ms}
	c.index()
	return c
}

// oneClass returns a single ClassShape that admits any model carrying text.
func oneClass(id, role string, volume float64) ClassShape {
	return ClassShape{ID: id, MinContext: 1, NeedsTools: false, NeedsImages: false,
		Volume: volume, Role: role}
}

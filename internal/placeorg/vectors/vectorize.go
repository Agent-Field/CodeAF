package vectors

import (
	"context"
	"fmt"
	"sync"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	// BatchSize is how many texts go to an Embedder in one call.
	BatchSize = 64
	// InFlight is how many batches may be out at once.
	InFlight = 4
)

// Vectorizer implements placeorg.Vectorizer over caches kept in one directory.
type Vectorizer struct {
	dir string
}

var _ placeorg.Vectorizer = (*Vectorizer)(nil)

// New returns a Vectorizer whose caches live in dir.
func New(dir string) *Vectorizer { return &Vectorizer{dir: dir} }

// Vectorize returns one vector per chat in corpus order, all from one space.
// It serves every chat already cached from the cache, embeds only the rest
// through primary, and on ANY failure there abandons primary for this run and
// embeds the whole corpus through local (still cache first), reporting
// fell = true. Vectors fetched from primary before the failure stay cached, so
// the next run with a healthy primary does not pay for them again.
func (v *Vectorizer) Vectorize(ctx context.Context, corpus *placeorg.Corpus, primary, local placeorg.Embedder) (placeorg.SpaceID, [][]float32, bool, error) {
	if primary != nil {
		vecs, err := v.through(ctx, corpus, primary)
		if err == nil {
			return primary.Space(), vecs, false, nil
		}
		if ctx.Err() != nil {
			return "", nil, false, ctx.Err()
		}
	}
	if local == nil {
		return "", nil, false, fmt.Errorf("vectors: primary space failed and no local embedder was supplied")
	}
	vecs, err := v.through(ctx, corpus, local)
	if err != nil {
		return "", nil, false, err
	}
	return local.Space(), vecs, true, nil
}

// through fills every chat's vector from one embedder's space.
func (v *Vectorizer) through(ctx context.Context, corpus *placeorg.Corpus, e placeorg.Embedder) ([][]float32, error) {
	store, err := Open(v.dir, e.Space(), e.Dims())
	if err != nil {
		return nil, err
	}
	defer store.Close()

	chats := corpus.Chats
	hashes := make([]string, len(chats))
	ids := make([]string, len(chats))
	for i, c := range chats {
		hashes[i], ids[i] = HashText(c.Text), c.ID
	}
	out, have := store.All(ids, hashes)
	var missing []int
	for i, ok := range have {
		if !ok {
			missing = append(missing, i)
		}
	}
	if err := v.fetch(ctx, store, e, chats, hashes, out, missing); err != nil {
		return nil, err
	}
	_, _ = store.Compact() // best effort: a failed compaction leaves a valid, larger file
	return out, nil
}

// fetch embeds the missing chats in batches, InFlight at a time. The first
// failure cancels the batches still running and is returned; what already
// arrived stays in the cache.
func (v *Vectorizer) fetch(ctx context.Context, store *Store, e placeorg.Embedder, chats []placeorg.Chat, hashes []string, out [][]float32, missing []int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
		sem      = make(chan struct{}, InFlight)
	)
	fail := func(err error) { once.Do(func() { firstErr = err; cancel() }) }
	for lo := 0; lo < len(missing); lo += BatchSize {
		batch := missing[lo:min(lo+BatchSize, len(missing))]
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			texts := make([]string, len(batch))
			for k, i := range batch {
				texts[k] = chats[i].Text
			}
			got, err := e.Embed(ctx, texts)
			if err == nil && len(got) != len(batch) {
				err = fmt.Errorf("vectors: %s returned %d vectors for %d texts", e.Space(), len(got), len(batch))
			}
			for k := 0; err == nil && k < len(batch); k++ {
				i := batch[k]
				if err = store.Put(chats[i].ID, hashes[i], got[k]); err == nil {
					out[i], _ = store.Get(chats[i].ID, hashes[i])
				}
			}
			if err != nil {
				fail(err)
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

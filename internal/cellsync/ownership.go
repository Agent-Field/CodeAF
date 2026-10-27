package cellsync

// Lease names the directory record and fence this batcher publishes under.
// After keeping orphaned turns, that record can be a branch rather than the
// local cell's ID; admission must compare the same lease as publishing does.
func (b *Batcher) Lease() (id string, fence uint64) {
	d, _, _ := b.view()
	return d.ID(), d.Fence
}

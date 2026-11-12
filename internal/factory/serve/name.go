package serve

// nameRepos reads only the saved selection, never the forge's repository
// catalogue. The display name is fixed at start for every caller and response.
func (h *Handler) nameRepos() []string {
	if h.config.Name != "" {
		return nil
	}
	if h.config.Store != nil {
		repos, _ := h.config.Store.Repos()
		return repos
	}
	if h.seam.Load == nil {
		return nil
	}
	snapshot, err := h.seam.Load()
	if err != nil || len(snapshot.Repos) == 0 {
		return nil
	}
	return []string{snapshot.Repos[0].Name}
}

package main

// beginClose keeps final publish news after the last live watch has retired.
func (n *driveNotices) beginClose() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closing = true
}

func (l *liveDrive) TakeSavingNotices() []string {
	if l == nil || l.notices == nil {
		return nil
	}
	n := l.notices
	n.mu.Lock()
	defer n.mu.Unlock()
	lines := n.final
	n.final = nil
	return lines
}

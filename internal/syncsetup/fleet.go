package syncsetup

import "context"

// FleetSize is how many devices this person has paired and not revoked, read
// from the directory. It is the fact the home card that offers another machine
// waits on.
func (s *Sync) FleetSize(ctx context.Context) (int, error) {
	l, err := s.Dir.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range l.Devices {
		if !d.Revoked {
			n++
		}
	}
	return n, nil
}

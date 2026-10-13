package teams

import "errors"

// UpdateClosing saves an app crew mutation and its closing-report decision
// under one lifecycle lock. The callback runs before any decision is written,
// so stale state, open work and stop failures cannot accept a report. The old
// document stays open until decideLocked records the person's answer; ordinary
// Decide rightly refuses a packet whose origin has already closed.
func UpdateClosing(profileDir string, change func(*File) (packetID, decision string, err error)) error {
	return withLock(profileDir, lockWait, func() error {
		_, err := updateLocked(profileDir, func(f *File) error {
			packetID, decision, err := change(f)
			if err != nil || packetID == "" {
				return err
			}
			packet, err := PacketByID(profileDir, packetID)
			if err != nil {
				return err
			}
			if packet.Kind != PacketClosing {
				return ErrNotClosing
			}
			if !packet.Waiting() {
				if packet.DecidedBy == Person && packet.Decision == decision {
					return nil
				}
				return ErrDecided
			}
			if decision != OptionClose && decision != OptionKeepGoing {
				return errors.New("teams: invalid closing decision")
			}
			_, err = decideLocked(profileDir, packetID, Person, decision, "")
			return err
		})
		return err
	})
}

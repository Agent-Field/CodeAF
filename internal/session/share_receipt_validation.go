package session

import "time"

// A missing or remote-future deadline cannot remove publication's IO budget.
func boundedShareDeadline(requested time.Time) time.Time {
	cap := time.Now().Add(shareOpTimeout)
	if requested.IsZero() || requested.After(cap) {
		return cap
	}
	return requested
}

func validateShareReceipt(r ShareReceipt) error {
	if r.Version != shareOpVersion || ValidateShareOperationID(r.OperationID) != nil || ValidateShareOperationID(r.Session) != nil || r.Provenance.Session != r.Session || r.WrittenAt.IsZero() {
		return ErrShareOpInvalid
	}
	switch r.Status {
	case ShareOpPending, ShareOpAccepted, ShareOpRefused, ShareOpUnknown:
	default:
		return ErrShareOpInvalid
	}
	if len(r.Planned) == 0 || len(r.Planned) > shareOpPathsMax {
		return ErrShareOpInvalid
	}
	return validateShareRows(r)
}

func validateShareRows(r ShareReceipt) error {
	paths := make([]string, len(r.Planned))
	ids := make(map[string]bool, len(r.Planned))
	for i, row := range r.Planned {
		if !ValidDeliverableID(row.ID) || ids[row.ID] {
			return ErrShareOpInvalid
		}
		ids[row.ID] = true
		paths[i] = row.Path
	}
	normalized, digest, err := NormalizeSharePayload(paths)
	if err != nil || digest != r.PayloadDigest {
		return ErrShareOpInvalid
	}
	for i := range paths {
		if normalized[i] != paths[i] {
			return ErrShareOpInvalid
		}
	}
	if err := validateShareProvenance(r.Provenance); err != nil {
		return err
	}
	return validateShareRecorded(r, ids)
}

func validateShareProvenance(p ShareProvenance) error {
	for _, value := range []string{p.Run, p.Turn, p.Call} {
		if value != "" && ValidateShareOperationID(value) != nil {
			return ErrShareOpInvalid
		}
	}
	return nil
}

func validateShareRecorded(r ShareReceipt, ids map[string]bool) error {
	if len(r.RecordedIDs) > len(r.Planned) {
		return ErrShareOpInvalid
	}
	seen := make(map[string]bool, len(r.RecordedIDs))
	for _, id := range r.RecordedIDs {
		if !ids[id] || seen[id] {
			return ErrShareOpInvalid
		}
		seen[id] = true
	}
	if r.Status == ShareOpAccepted && len(r.RecordedIDs) != len(r.Planned) {
		return ErrShareOpInvalid
	}
	return nil
}

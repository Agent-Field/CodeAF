// The one-way state of a request (docs/ux-pairing-contract.md section 1): pending moves once, to
// approved or denied, and a repeat of the same decision changes nothing. Pure, so every row of the
// table is tested without a Durable Object.
import { alreadyDecided } from './refusals.js';

/**
 * settled answers the record after a decision: the same record when the decision repeats one already
 * made (the caller has already checked an approve is for this request's device), a new record when it is the first, and
 * throws already_decided when it contradicts the first.
 */
export function settled(record, state, grant, now) {
  if (record.state === 'pending') return { ...record, state, grant: state === 'approved' ? grant : null, decided_at: now };
  if (record.state === state) return record;
  throw alreadyDecided();
}

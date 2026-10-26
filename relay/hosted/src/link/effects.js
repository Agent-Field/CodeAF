import { alreadyDecided, gone } from './refusals.js';

// A public code belongs to one committed namespace once reserved. Reservations
// survive a failed proposal until the ordinary request expiry; another identity
// cannot create a second successful decision while the first is uncertain.
export function boundLink(record, cluster) {
  if (!record) throw gone();
  if (!cluster || (record.cluster && record.cluster !== cluster) || (!record.cluster && record.state !== 'pending')) throw alreadyDecided();
  return record.cluster ? record : { ...record, cluster };
}

// This is effect delivery only. Go already decided and installed authority.
// The exact id and body are checked so a lost RPC reply is safe to replay.
export function deliveredLink(record, cluster, id, state, grant, now, device) {
  if (!record) throw gone();
  if (!cluster || record.cluster !== cluster || record.device !== device || !id || !['approved', 'denied'].includes(state)) throw alreadyDecided();
  grant = state === 'approved' ? grant : null;
  if (record.delivery) {
    if (record.delivery === id && record.state === state && record.grant === grant) return record;
    throw alreadyDecided();
  }
  if (record.state !== 'pending' || (state === 'approved' && !grant)) throw alreadyDecided();
  return { ...record, state, grant, delivery: id, decided_at: now };
}

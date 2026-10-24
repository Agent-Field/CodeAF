// The rotation state machine of one identity, a port of internal/directory/rotation.go. An identity
// with no rotation is live. Freezing makes it read-only and is first come: the device that froze it
// owns the rotation, so two holders of the root cannot both be rotating it. Retiring makes the
// freeze permanent and names the time the relay deletes the identity. Everything here is pure: the
// relay's clock and limits come in as arguments, and the answer is the next rotation or a refusal.
import { RuleError } from './rules.js';

const refuse = (code) => new RuleError(code);

function freeze(cur, device, _req, now) {
  if (!cur) return { state: 'frozen', by: device, at: now };
  if (cur.by === device) return cur;
  throw refuse('rotated');
}

// Any device of the identity may thaw, because that is how a computer undoes a rotation whose
// keeper lost its disk. A retire cannot be undone by anyone.
function thaw(cur) {
  if (cur?.state === 'retired') throw refuse('rotated');
  return undefined;
}

function retire(cur, device, req, now, limits) {
  if (!cur) throw refuse('rotation_step');
  if (cur.by !== device) throw refuse('rotated');
  if (cur.state === 'retired') return cur;
  return { ...cur, state: 'retired', retire_at: now + graceOf(req, limits) };
}

// A grace of 0 (or none) asks for the relay's default; anything else must sit inside its bounds.
function graceOf({ grace_ms: asked = 0 }, { minGraceMs, maxGraceMs, defaultGraceMs }) {
  if (!Number.isInteger(asked)) throw refuse('bad_request');
  const grace = asked || defaultGraceMs;
  if (grace < minGraceMs || grace > maxGraceMs) throw refuse('bad_grace');
  return grace;
}

const STEPS = { freeze, thaw, retire };

/** rotateBy answers the rotation (undefined for live) after `device` makes the move `req`, or throws the refusal. */
export function rotateBy(cur, device, req, now, limits) {
  if (!Object.hasOwn(STEPS, req.op)) throw refuse('bad_request');
  return STEPS[req.op](cur, device, req, now, limits);
}

/** rotationView is the answer to both rotation calls: the state now, the directory's time, and the grace bounds the relay enforces. */
export const rotationView = (rotation, now, limits) => ({
  now,
  rotation,
  min_grace_ms: limits.minGraceMs,
  max_grace_ms: limits.maxGraceMs,
  default_grace_ms: limits.defaultGraceMs,
});

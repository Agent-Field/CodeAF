// The link wire's refusals (docs/ux-pairing-contract.md, sections 3 and 7): one status and one word each.
import { Wire } from '../wire.js';

export const gone = () => new Wire('gone', 404);
export const alreadyDecided = () => new Wire('already_decided', 409);
export const tooBig = () => new Wire('too_big', 413);
export const relayFull = () => new Wire('full', 503);
export const slowDown = (seconds) => new Wire('rate_limited', 429, seconds);
// A phone's approval needs the phone registered as a client of this identity and its enrollment stored first.
export const notEnrolled = () => new Wire('not_enrolled', 409);

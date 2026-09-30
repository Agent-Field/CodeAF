// The pairing wire's refusals. Each is one status and the word its body carries, exactly the table
// of internal/pairbox (wire.go): the two relays must not differ in a status or a word.
import { Wire } from '../wire.js';

export const forbidden = () => new Wire('forbidden', 403);
export const gone = () => new Wire('gone', 404);
export const sideFull = () => new Wire('full', 409);
export const tooBig = () => new Wire('too big', 413);
export const relayFull = () => new Wire('full', 503);
export const slowDown = (seconds) => new Wire('slow down', 429, seconds);

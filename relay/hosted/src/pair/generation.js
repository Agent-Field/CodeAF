// The generation of a mailbox: a random name for one opening of it, told to every device that touches it.
// A nameplate can be drawn again after its quarantine, and a Durable Object found by that name is the same
// object, so a device holding an older mailbox's code could be answered by the new one. A device that carries
// the generation it was told is refused with `gone` when the mailbox under the plate is a different one.
// The header is optional in both directions: a device that sends none, or a mailbox opened before generations
// existed, is not fenced, so old builds go on working exactly as they did.
import { hex } from '../codec.js';
import { gone } from './refusals.js';

export const GENERATION_HEADER = 'codeaf-pair-gen';
const SHAPE = /^[0-9a-f]{32}$/;

/** newGeneration draws 128 random bits as 32 hex characters. */
export function newGeneration() {
  return hex(crypto.getRandomValues(new Uint8Array(16)));
}

/** generationOf is the generation a request carries: null when it carries none, and a value no box has when it is malformed. */
export function generationOf(request) {
  const text = request.headers.get(GENERATION_HEADER);
  if (text === null) return null;
  return SHAPE.test(text) ? text : '!';
}

/** fence throws gone when `box` is a different opening from the one `asked` names; a missing side of the comparison lets the call through. */
export function fence(box, asked) {
  if (asked !== null && asked !== undefined && box.gen && box.gen !== asked) throw gone();
}

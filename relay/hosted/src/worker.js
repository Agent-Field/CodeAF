// The hosted relay's front door: a stateless Worker that decides where a request goes and does
// nothing else. Pairing goes to its own objects; the store and directory wires go to the
// Durable Object of the identity named in the request's unsigned header, and the body is not
// read here. That routing before reading is what keeps one identity's slow put from ever
// delaying another identity's Has, and what lets an identity's object count its own puts.
import { dispatch } from './front.js';
import { respond } from './wire.js';

export { IdentityDO } from './identity.js';
export { NewcomerGate } from './newcomers.js';
export { LinkGate } from './link/gate.js';
export { LinkRequest } from './link/request.js';
export { PairGate } from './pair/gate.js';
export { Mailbox } from './pair/mailbox.js';

export default {
  fetch: (request, env) => respond(() => dispatch(request, env)),
};

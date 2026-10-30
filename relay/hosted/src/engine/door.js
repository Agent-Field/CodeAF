// The front door's half of the machine doors: refuse what needs no object to refuse, then hand the upgrade to the
// one object that holds that machine name (machine.js).
import { validName } from './names.js';
import { DIAL_PREFIX, ENGINE_PATH, HEADER_NAME, PROTOCOL, plain } from './carrier.js';

/** isMachineDoor is a path this module serves. */
export const isMachineDoor = (pathname) => pathname === ENGINE_PATH || pathname.startsWith(DIAL_PREFIX);

/**
 * serveMachineDoor routes a registration or a dial. Only a WebSocket offering the carrier's protocol is taken: a
 * Worker cannot answer codeaf's raw upgrade, and the 426 is what tells a client to use the WebSocket.
 */
export async function serveMachineDoor(request, env) {
  const offered = (request.headers.get('sec-websocket-protocol') ?? '').split(',').map((p) => p.trim());
  if ((request.headers.get('upgrade') ?? '').toLowerCase() !== 'websocket' || !offered.includes(PROTOCOL)) {
    return plain(426, `this door speaks ${PROTOCOL} over a WebSocket`);
  }
  const { pathname } = new URL(request.url);
  const name = pathname === ENGINE_PATH ? request.headers.get(HEADER_NAME) ?? '' : pathname.slice(DIAL_PREFIX.length);
  if (!validName(name)) return plain(400, 'that is not a machine name');
  const answer = await env.MACHINE.get(env.MACHINE.idFromName(name)).fetch(request);
  // An object's answer has immutable headers, and the Worker stamps Codeaf-Now on every answer.
  return new Response(answer.body, { status: answer.status, headers: answer.headers, webSocket: answer.webSocket });
}

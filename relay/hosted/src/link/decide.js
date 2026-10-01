// Approve and deny (docs/ux-pairing-contract.md 3.3 and 3.4): the two signed routes by which a paired
// device answers a request. The request object decides once, so two approvers racing get one 204 and
// one 409. Only after that does the identity write the device, in one synchronous turn; because a
// repeat of the same approve decides nothing new and writes the same device again, a crash between
// the two steps is mended by the caller's retry.
import { checkCert } from '../verify.js';
import { badRequest, unwrap } from '../wire.js';
import { asGrant, codeOf, platformOf } from './code.js';
import { gone } from './refusals.js';

const gateOf = (c) => c.env.LINK_GATE.get(c.env.LINK_GATE.idFromName('gate'));

function requestOf(c, text) {
  const code = codeOf(text);
  if (!code) throw gone();
  return { code, box: c.env.LINK_REQUEST.get(c.env.LINK_REQUEST.idFromName(code)) };
}

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

/** certDevice is the device id a cert proves under the approver's own identity key, which is what makes the cert "for" this identity. */
async function certDevice(c, cert) {
  if (typeof cert !== 'string') throw badRequest();
  const proof = await checkCert({ identity: c.request.headers.get('codeaf-identity'), cert, time: '0', sig: '0' }).catch(() => null);
  if (proof?.identity !== c.tenant.identity) throw badRequest();
  return proof.device;
}

/** asApproval checks the approve body against the request it answers, and answers the device record to write. */
async function asApproval(c, body, request) {
  if (!isObject(body.device)) throw badRequest();
  if ((await certDevice(c, body.cert)) !== request.device) throw badRequest();
  if (c.tenant.dir.revoked(request.device)) throw badRequest(); // a revoked device re-joins only as a new request and a new key
  const grant = asGrant(body.grant, c.tenant.limits.linkMaxGrant);
  return { grant, device: { ...body.device, platform: platformOf(body.device.platform ?? request.platform) } };
}

/** approve answers the 204 of a request approved, or throws the refusal that says why not. */
export async function approve(c, [text]) {
  const { code, box } = requestOf(c, text);
  const request = unwrap(await box.read(0));
  const { grant, device } = await asApproval(c, parseJson(c.body), request);
  unwrap(await box.decide('approved', grant));
  c.tenant.join(request.device, device);
  await gateOf(c).decided(code);
}

/** deny answers the 204 of a request declined. */
export async function deny(c, [text]) {
  const { code, box } = requestOf(c, text);
  unwrap(await box.decide('denied', null));
  await gateOf(c).decided(code);
}

function parseJson(bytes) {
  const body = JSON.parse(new TextDecoder().decode(bytes));
  if (!isObject(body)) throw badRequest();
  return body;
}

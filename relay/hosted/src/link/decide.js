// Approve and deny (docs/ux-pairing-contract.md 3.3 and 3.4): the two signed routes by which a paired
// device answers a request. The request object decides once, so two approvers racing get one 204 and
// one 409. Only after that does the identity write the device, in one synchronous turn; because a
// repeat of the same approve decides nothing new and writes the same device again, a crash between
// the two steps is mended by the caller's retry.
import { checkCert } from '../verify.js';
import { fromBase64Url } from '../codec.js';
import { badRequest, unwrap } from '../wire.js';
import { asGrant, CLIENT_CLASS, codeOf, platformOf } from './code.js';
import { gone, notEnrolled } from './refusals.js';

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

/**
 * checkApprovalDevice validates the certificate before a request can be reserved. It is the full-device
 * body of a computer's request: a body that names a `class`, or any body for a phone's request, is 400
 * here, so a phone's request can never be turned into a device with an identity's authority.
 */
export async function checkApprovalDevice(c, body, request) {
  if (!isObject(body) || !isObject(body.device)) throw badRequest();
  if (body.class !== undefined || request.class !== undefined) throw badRequest();
  if ((body.device.kind ?? '') !== '' || (body.device.methods?.length ?? 0) > 0) throw badRequest();
  if ((await certDevice(c, body.cert)) !== request.device) throw badRequest();
}

/** asApproval checks the approve body against the request it answers, and answers the device record to write. */
async function asApproval(c, body, request) {
  await checkApprovalDevice(c, body, request);
  if (c.tenant.dir.revoked(request.device)) throw badRequest(); // a revoked device re-joins only as a new request and a new key
  const grant = asGrant(body.grant, c.tenant.limits.linkMaxGrant);
  return { grant, device: { ...body.device, platform: platformOf(body.device.platform ?? request.platform) } };
}

/**
 * clientGrant checks the body of a phone's approval: exactly {"class":"client","grant":<base64url>}, with
 * the grant at most `max` bytes decoded. Every refusal here is 400 (routes.json link.approve.client), the
 * oversize grant too, unlike a computer's approval, which answers that 413 too_big.
 */
export function clientGrant(body, max) {
  if (!isObject(body)) throw badRequest();
  const keys = Object.keys(body);
  if (keys.length !== 2 || body.class !== CLIENT_CLASS || !keys.includes('grant')) throw badRequest();
  const bytes = typeof body.grant === 'string' ? fromBase64Url(body.grant) : null;
  if (!bytes || bytes.length === 0 || bytes.length > max) throw badRequest();
  return body.grant;
}

/**
 * requireEnrolled is the precondition of a phone's approval: the device is registered as a client of this
 * identity, not revoked, and its enrollment is stored. The approver enrolls and registers first (design
 * 3.7), because the approval itself writes no device row. Otherwise 409 not_enrolled and the request
 * stays pending.
 */
export function requireEnrolled(row, enrolled) {
  if (row?.kind !== 'client' || row.revoked || !enrolled) throw notEnrolled();
}

/** approve answers the 204 of a request approved, or throws the refusal that says why not. */
export async function approve(c, [text]) {
  const { code, box } = requestOf(c, text);
  const request = unwrap(await box.read(0));
  if (request.class === CLIENT_CLASS) return approveClient(c, code, box, request);
  const { grant, device } = await asApproval(c, parseJson(c.body), request);
  unwrap(await box.decide('approved', grant));
  c.tenant.join(request.device, device);
  await gateOf(c).decided(code);
}

// A phone's approval writes no device row: it marks the request approved and keeps the grant for the
// phone's read. Nothing here reaches the directory, so there is nothing for the phone to be mistaken for.
async function approveClient(c, code, box, request) {
  const grant = clientGrant(parseJson(c.body), c.tenant.limits.linkMaxGrant);
  requireEnrolled(c.tenant.dir.read('devices', request.device), c.tenant.fabric.hasEnrollment(request.device));
  unwrap(await box.decide('approved', grant));
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

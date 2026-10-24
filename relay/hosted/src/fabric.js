import { base64, fromBase64Url, hex, sha256 } from './codec.js';
import { SKEW_MS, Refusal } from './verify.js';
import { Wire, badRequest, notFound } from './wire.js';

const DEVICE = /^dev_[0-9a-f]{32}$/;
const NAME = /^[A-Za-z0-9_.-]{1,128}$/;
const CERT_LABEL = 'codeaf/device-cert/v1\n';
const decoder = new TextDecoder();
const encoder = new TextEncoder();

export const MAX_FABRIC_BODY = 96 << 10;
const MAX_CLIENT_METHODS = 64;
const MAX_CLIENT_NAME = 256;
const MAX_SLOT_BYTES = 64 << 10;
const MAX_CLIENT_SLOTS = 64;
const MAX_QUEUE_BYTES = 4096;
const MAX_CLIENT_QUEUE = 128;
const FABRIC_MAX_AGE_MS = 24 * 60 * 60 * 1000;

const claimed = () => new Wire('claimed', 409);
const stale = () => new Wire('stale', 400);
const full = () => new Wire('full', 429);
const exists = () => new Wire('exists', 409);
const sequence = () => new Wire('sequence', 409);
const methodDenied = () => new Wire('method_denied', 403);
const tooLarge = () => new Wire('too_large', 413);
const unauthorized = () => new Refusal('unauthorized', 'unauthorized');

const fromHex = (text) => {
  if (typeof text !== 'string' || !/^([0-9a-f]{2})*$/.test(text)) return null;
  return Uint8Array.from(text.match(/../g) ?? [], (h) => parseInt(h, 16));
};

const fromBase64 = (text) => {
  if (typeof text !== 'string' || text.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(text)) return null;
  try {
    return Uint8Array.from(atob(text), (c) => c.charCodeAt(0));
  } catch {
    return null;
  }
};

const certBody = (cert) => encoder.encode(CERT_LABEL + JSON.stringify({ V: cert.V, device: cert.device, created: cert.created, sig: '' }));

function parseObject(body) {
  let value;
  try {
    value = JSON.parse(decoder.decode(body));
  } catch {
    throw badRequest();
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw badRequest();
  return value;
}

function expectKeys(body, required, optional = []) {
  const obj = parseObject(body);
  const allowed = new Set([...required, ...optional]);
  for (const key of Object.keys(obj)) {
    if (!allowed.has(key)) throw badRequest();
  }
  for (const key of required) {
    if (!(key in obj)) throw badRequest();
  }
  return obj;
}

const ensureDevice = (device) => {
  if (!DEVICE.test(device)) throw badRequest();
};

const ensureName = (name) => {
  if (typeof name !== 'string' || !NAME.test(name)) throw badRequest();
};

const asInt = (value) => {
  if (typeof value !== 'number' || !Number.isInteger(value) || !Number.isSafeInteger(value)) throw badRequest();
  return value;
};

const parseEntryNumber = (text) => {
  if (typeof text !== 'string' || !/^[1-9][0-9]*$/.test(text)) throw badRequest();
  const number = Number(text);
  if (!Number.isSafeInteger(number)) throw badRequest();
  return number;
};

const verifyClientCert = async (cert, identityHeader, targetDevice) => {
  const key = fromBase64Url(identityHeader ?? '');
  if (!key || key.length !== 32) throw badRequest();
  if (!cert || typeof cert !== 'object' || Array.isArray(cert)) throw badRequest();
  if (new Set(Object.keys(cert)).size !== Object.keys(cert).length) throw badRequest();
  for (const keyName of Object.keys(cert)) {
    if (!new Set(['V', 'device', 'created', 'sig']).has(keyName)) throw badRequest();
  }
  if (!('V' in cert) || !('device' in cert) || !('created' in cert) || !('sig' in cert)) throw badRequest();
  const devicePub = fromHex(cert.device);
  const sig = fromHex(cert.sig);
  if (!devicePub || !sig || devicePub.length !== 32) throw badRequest();
  const imported = await crypto.subtle.importKey('raw', key, { name: 'Ed25519' }, false, ['verify']);
  const ok = await crypto.subtle.verify({ name: 'Ed25519' }, imported, sig, certBody(cert));
  if (!ok) throw badRequest();
  const id = 'dev_' + hex((await sha256(devicePub)).subarray(0, 16));
  if (id !== targetDevice) throw badRequest();
};

export class Fabric {
  constructor(sql, clock, dir) {
    this.sql = sql;
    this.clock = clock;
    this.dir = dir;
    sql.exec(
      'CREATE TABLE IF NOT EXISTS fabric_slots (device TEXT NOT NULL, name TEXT NOT NULL, sealed TEXT NOT NULL, writer TEXT NOT NULL, at INTEGER NOT NULL, PRIMARY KEY (device, name)) WITHOUT ROWID',
    );
    sql.exec(
      'CREATE TABLE IF NOT EXISTS fabric_queue (device TEXT NOT NULL, number INTEGER NOT NULL, at INTEGER NOT NULL, method TEXT NOT NULL, target TEXT NOT NULL, sealed TEXT NOT NULL, claimed_by TEXT NOT NULL, signature TEXT NOT NULL, cert TEXT NOT NULL, identity TEXT NOT NULL, request_time TEXT NOT NULL, request_uri TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY (device, number)) WITHOUT ROWID',
    );
    sql.exec('CREATE TABLE IF NOT EXISTS fabric_seq (device TEXT PRIMARY KEY, last_number INTEGER NOT NULL) WITHOUT ROWID');
  }

  isClient(device) {
    return this.dir.read('devices', device)?.kind === 'client';
  }

  revokeClient(device) {
    this.sql.exec('DELETE FROM fabric_slots WHERE device = ?', device);
    this.sql.exec('DELETE FROM fabric_queue WHERE device = ?', device);
    this.sql.exec('DELETE FROM fabric_seq WHERE device = ?', device);
  }

  async registerClient({ caller, targetDevice, body, identityHeader, identity }) {
    ensureDevice(targetDevice);
    this.#requireFullCaller(caller);
    const inBody = expectKeys(body, ['cert', 'methods'], ['name']);
    if (!Array.isArray(inBody.methods)) throw badRequest();
    const name = inBody.name ?? '';
    if (typeof name !== 'string') throw badRequest();
    if (inBody.methods.length > MAX_CLIENT_METHODS || name.length > MAX_CLIENT_NAME) throw tooLarge();
    const seen = new Set();
    for (const method of inBody.methods) {
      if (typeof method !== 'string' || !NAME.test(method) || seen.has(method)) throw badRequest();
      seen.add(method);
    }
    await verifyClientCert(inBody.cert, identityHeader, targetDevice);
    if (this.dir.read('devices', targetDevice)) throw exists();
    this.dir.putDevice(targetDevice, { V: 1, kind: 'client', methods: inBody.methods, name, added_by: identity });
  }

  putSlot({ caller, targetDevice, name, body }) {
    ensureDevice(targetDevice);
    ensureName(name);
    this.#requireFullCaller(caller);
    this.#requireTargetClient(targetDevice);
    const inBody = expectKeys(body, ['sealed']);
    const sealed = fromBase64(inBody.sealed);
    if (!sealed) throw badRequest();
    if (sealed.length > MAX_SLOT_BYTES) throw tooLarge();
    if (sealed.length === 0) throw badRequest();
    this.#prune(targetDevice);
    const hasSlot = this.sql.exec('SELECT 1 FROM fabric_slots WHERE device = ? AND name = ?', targetDevice, name).toArray().length > 0;
    if (!hasSlot && this.#slotCount(targetDevice) >= MAX_CLIENT_SLOTS) throw full();
    this.sql.exec(
      'INSERT OR REPLACE INTO fabric_slots VALUES (?,?,?,?,?)',
      targetDevice,
      name,
      base64(sealed),
      caller,
      this.clock(),
    );
  }

  getSlot({ caller, targetDevice, name, body }) {
    ensureDevice(targetDevice);
    ensureName(name);
    this.#requireClientCaller(caller, targetDevice);
    this.#requireTargetClient(targetDevice);
    if (body.length !== 0) throw badRequest();
    this.#prune(targetDevice);
    const row = this.sql.exec('SELECT sealed, writer, at FROM fabric_slots WHERE device = ? AND name = ?', targetDevice, name).toArray()[0];
    if (!row) throw notFound();
    return { sealed: row.sealed, writer: row.writer, at: row.at };
  }

  appendQueue({ caller, targetDevice, body, proof }) {
    ensureDevice(targetDevice);
    this.#requireClientCaller(caller, targetDevice);
    this.#requireTargetClient(targetDevice);
    const inBody = expectKeys(body, ['number', 'at', 'method', 'target', 'sealed']);
    const number = asInt(inBody.number);
    const at = asInt(inBody.at);
    if (number <= 0) throw sequence();
    if (typeof inBody.method !== 'string' || !NAME.test(inBody.method)) throw badRequest();
    const callerRec = this.dir.read('devices', caller);
    if (!Array.isArray(callerRec?.methods) || !callerRec.methods.includes(inBody.method)) throw methodDenied();
    const target = inBody.target;
    if (typeof target !== 'string') throw badRequest();
    if (target !== 'any') {
      if (!DEVICE.test(target)) throw badRequest();
      const rec = this.dir.read('devices', target);
      if (!rec || rec.kind === 'client' || rec.revoked === true) throw badRequest();
    }
    const sealed = fromBase64(inBody.sealed);
    if (!sealed) throw badRequest();
    if (sealed.length > MAX_QUEUE_BYTES) throw tooLarge();
    if (sealed.length === 0) throw badRequest();
    const now = this.clock();
    if (at <= now - FABRIC_MAX_AGE_MS || at > now + SKEW_MS) throw stale();
    this.#prune(targetDevice);
    const last = this.sql.exec('SELECT last_number FROM fabric_seq WHERE device = ?', targetDevice).toArray()[0]?.last_number ?? 0;
    if (number !== last + 1) throw sequence();
    if (this.#queueCount(targetDevice) >= MAX_CLIENT_QUEUE) throw full();
    this.sql.exec(
      'INSERT INTO fabric_queue VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)',
      targetDevice,
      number,
      at,
      inBody.method,
      target,
      base64(sealed),
      '',
      proof.signature,
      proof.cert,
      proof.identity,
      proof.requestTime,
      proof.requestUri,
      base64(proof.bodyBytes),
    );
    this.sql.exec('INSERT INTO fabric_seq VALUES (?,?) ON CONFLICT(device) DO UPDATE SET last_number = excluded.last_number', targetDevice, number);
  }

  listQueue({ caller, targetDevice, body }) {
    ensureDevice(targetDevice);
    this.#requireFullCaller(caller);
    this.#requireTargetClient(targetDevice);
    if (body.length !== 0) throw badRequest();
    this.#prune(targetDevice);
    const rows = this.sql
      .exec(
        'SELECT number, at, method, target, sealed, signature, cert, identity, request_time, request_uri, body FROM fabric_queue WHERE device = ? AND (target = ? OR target = ?) AND (claimed_by = ? OR claimed_by = ? OR claimed_by GLOB ?) ORDER BY number',
        targetDevice,
        'any',
        caller,
        '',
        caller,
        caller + ':*',
      )
      .toArray();
    return rows.map((row) => ({
      number: row.number,
      at: row.at,
      method: row.method,
      target: row.target,
      sealed: row.sealed,
      signature: row.signature,
      cert: row.cert,
      identity: row.identity,
      request_time: row.request_time,
      request_uri: row.request_uri,
      body: row.body,
    }));
  }

  claimQueue({ caller, targetDevice, numberText, body, ack }) {
    ensureDevice(targetDevice);
    this.#requireFullCaller(caller);
    this.#requireTargetClient(targetDevice);
    let token = '';
    if (body.length !== 0) {
      if (ack) throw badRequest();
      const params = expectKeys(body, ['token']);
      if (typeof params.token !== 'string' || !/^[0-9a-f]{64}$/.test(params.token)) throw badRequest();
      token = params.token;
    }
    const number = parseEntryNumber(numberText);
    this.#prune(targetDevice);
    const row = this.sql
      .exec('SELECT target, claimed_by FROM fabric_queue WHERE device = ? AND number = ?', targetDevice, number)
      .toArray()[0];
    if (!row) throw notFound();
    if (row.target !== 'any' && row.target !== caller) throw unauthorized();
    const owner = row.claimed_by.split(':')[0];
    const claim = token === '' ? caller : caller + ':' + token;
    if ((!ack && row.claimed_by !== '' && (token === '' || row.claimed_by !== claim)) || (ack && owner !== caller)) throw claimed();
    if (ack) this.sql.exec('DELETE FROM fabric_queue WHERE device = ? AND number = ?', targetDevice, number);
    else this.sql.exec('UPDATE fabric_queue SET claimed_by = ? WHERE device = ? AND number = ?', claim, targetDevice, number);
  }

  #requireFullCaller(caller) {
    if (this.isClient(caller)) throw unauthorized();
  }

  #requireClientCaller(caller, targetDevice) {
    if (!this.isClient(caller) || caller !== targetDevice) throw unauthorized();
  }

  #requireTargetClient(targetDevice) {
    const target = this.dir.read('devices', targetDevice);
    if (!target || target.kind !== 'client' || target.revoked === true) throw notFound();
  }

  #slotCount(device) {
    return this.sql.exec('SELECT COUNT(*) AS n FROM fabric_slots WHERE device = ?', device).one().n;
  }

  #queueCount(device) {
    return this.sql.exec('SELECT COUNT(*) AS n FROM fabric_queue WHERE device = ?', device).one().n;
  }

  #prune(device) {
    const cutoff = this.clock() - FABRIC_MAX_AGE_MS;
    this.sql.exec('DELETE FROM fabric_slots WHERE device = ? AND at <= ?', device, cutoff);
    this.sql.exec('DELETE FROM fabric_queue WHERE device = ? AND at <= ?', device, cutoff);
  }
}

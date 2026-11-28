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
// Each computer writes a card, an outcome slot and up to ten read snapshots, twelve in all, so 128 serves ten
// computers; the first wire allowed 64, which is five (clientproto.MaxClientSlots).
const MAX_CLIENT_SLOTS = 128;
const MAX_QUEUE_BYTES = 4096;
const MAX_CLIENT_QUEUE = 128;
const FABRIC_MAX_AGE_MS = 24 * 60 * 60 * 1000;

// The enrollment an identity signs to admit one phone (clientproto.Enrollment): what is stored, how many, how long.
const ENROLL_LABEL = 'codeaf-enroll-v1\n';
const ENROLL_KEYS = ['v', 'device', 'sign_pub', 'box_pub', 'methods', 'reads', 'ver', 'sig'];
const MAX_ENROLLMENTS = 16;
const MAX_ENROLL_NAMES = 64;

// A slot wait is held at most this long, and a client may park a few at once so a dying connection can be replaced
// without one client using up the identity's share (clientproto.MaxSlotWait, SlotWaitersPerClient).
export const MAX_SLOT_WAIT_SECONDS = 25;
export const SLOT_WAITERS_PER_CLIENT = 2;

const claimed = () => new Wire('claimed', 409);
const stale = () => new Wire('stale', 400);
const full = () => new Wire('full', 429);
const exists = () => new Wire('exists', 409);
const versionConflict = () => new Wire('version', 409);
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

// A list of names is short, made of well-formed names, and has no repeat (clientproto names()).
const isNameList = (list) => {
  if (!Array.isArray(list) || list.length > MAX_ENROLL_NAMES) return false;
  const seen = new Set();
  for (const name of list) {
    if (typeof name !== 'string' || !NAME.test(name) || seen.has(name)) return false;
    seen.add(name);
  }
  return true;
};

// The text the identity's signature covers: no JSON canonicalization, and the two lists sorted by byte so the
// order a signer listed them in cannot change it (clientproto Enrollment.SignText). Names are ASCII, so a code
// unit sort is a byte sort.
const enrollText = (device, signPub, boxPub, methods, reads, ver) =>
  encoder.encode(
    ENROLL_LABEL + device + '\n' + hex(signPub) + '\n' + hex(boxPub) + '\n' + [...methods].sort().join(',') + '\n' + [...reads].sort().join(',') + '\n' + ver,
  );

/**
 * checkEnrollment answers the enrollment a body states, in the form it is stored and listed, or throws bad_request. It
 * checks the shape and the identity's signature, and nothing about the directory: the device need not be registered.
 * The keys and the signature are re-encoded, so two spellings of the same bytes are one statement.
 */
export async function checkEnrollment(body, identityHeader, targetDevice) {
  const e = expectKeys(body, ENROLL_KEYS);
  if (e.v !== 1 || !Number.isSafeInteger(e.ver) || e.ver < 1) throw badRequest();
  if (typeof e.device !== 'string' || e.device !== targetDevice) throw badRequest();
  const signPub = fromBase64(e.sign_pub);
  const boxPub = fromBase64(e.box_pub);
  const sig = fromBase64(e.sig);
  if (signPub?.length !== 32 || boxPub?.length !== 32 || sig?.length !== 64) throw badRequest();
  if (e.device !== 'dev_' + hex((await sha256(signPub)).subarray(0, 16))) throw badRequest();
  if (!isNameList(e.methods) || !isNameList(e.reads)) throw badRequest();
  const identity = fromBase64Url(identityHeader ?? '');
  if (!identity || identity.length !== 32) throw badRequest();
  const key = await crypto.subtle.importKey('raw', identity, { name: 'Ed25519' }, false, ['verify']);
  if (!(await crypto.subtle.verify({ name: 'Ed25519' }, key, sig, enrollText(e.device, signPub, boxPub, e.methods, e.reads, e.ver)))) throw badRequest();
  return {
    v: 1,
    device: e.device,
    sign_pub: base64(signPub),
    box_pub: base64(boxPub),
    methods: e.methods,
    reads: e.reads,
    ver: e.ver,
    sig: base64(sig),
  };
}

// Two enrollments are one statement when every field is equal, the lists compared as sets because the signature covers them sorted.
const sameSet = (a, b) => a.length === b.length && [...a].sort().join('\n') === [...b].sort().join('\n');
const sameEnrollment = (a, b) =>
  a.v === b.v && a.device === b.device && a.ver === b.ver && a.sign_pub === b.sign_pub && a.box_pub === b.box_pub && a.sig === b.sig &&
  sameSet(a.methods, b.methods) && sameSet(a.reads, b.reads);

// A decimal that fits a uint64, the way Go's ParseUint reads one; BigInt because a version past 2^53 is still a version.
const UINT64_MAX = (1n << 64n) - 1n;
const parseSince = (text) => {
  if (typeof text !== 'string' || !/^[0-9]+$/.test(text)) throw badRequest();
  const value = BigInt(text);
  if (value > UINT64_MAX) throw badRequest();
  return value;
};

// A number of seconds in 0 to 25 as Go's ParseFloat reads one (a plain decimal, a fraction or an exponent), and nothing else.
const parseWaitSeconds = (text) => {
  if (typeof text !== 'string' || !/^\+?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$/.test(text)) throw badRequest();
  const seconds = Number(text);
  if (!Number.isFinite(seconds) || seconds < 0 || seconds > MAX_SLOT_WAIT_SECONDS) throw badRequest();
  return seconds;
};

export class Fabric {
  // whenRevoked is told the device of every revokeClient, so the host can end what is parked for it. It is one
  // seam that the host sets, not a list, so a host built twice over one tenant still tells it once.
  whenRevoked = () => {};

  constructor(sql, clock, dir) {
    this.sql = sql;
    this.clock = clock;
    this.dir = dir;
    // version is the value of the client's slot counter at this slot's last PUT; a table made before it existed gains it below.
    sql.exec(
      'CREATE TABLE IF NOT EXISTS fabric_slots (device TEXT NOT NULL, name TEXT NOT NULL, sealed TEXT NOT NULL, writer TEXT NOT NULL, at INTEGER NOT NULL, version INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (device, name)) WITHOUT ROWID',
    );
    // CREATE TABLE IF NOT EXISTS never adds a column to a table that exists, so ask for the column and add it when the table lacks it.
    try {
      sql.exec('SELECT version FROM fabric_slots LIMIT 0');
    } catch {
      sql.exec('ALTER TABLE fabric_slots ADD COLUMN version INTEGER NOT NULL DEFAULT 0');
    }
    // One counter per client, raised by one on every slot PUT: the slot version of routes.json slots.watch.
    sql.exec('CREATE TABLE IF NOT EXISTS fabric_versions (device TEXT PRIMARY KEY, version INTEGER NOT NULL) WITHOUT ROWID');
    sql.exec(
      'CREATE TABLE IF NOT EXISTS fabric_queue (device TEXT NOT NULL, number INTEGER NOT NULL, at INTEGER NOT NULL, method TEXT NOT NULL, target TEXT NOT NULL, sealed TEXT NOT NULL, claimed_by TEXT NOT NULL, signature TEXT NOT NULL, cert TEXT NOT NULL, identity TEXT NOT NULL, request_time TEXT NOT NULL, request_uri TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY (device, number)) WITHOUT ROWID',
    );
    sql.exec('CREATE TABLE IF NOT EXISTS fabric_seq (device TEXT PRIMARY KEY, last_number INTEGER NOT NULL) WITHOUT ROWID');
    // The signed enrollment of each phone, as the JSON text that is listed back. It is kept after a revoke, so a version cannot roll back.
    sql.exec('CREATE TABLE IF NOT EXISTS fabric_enroll (device TEXT PRIMARY KEY, enroll TEXT NOT NULL) WITHOUT ROWID');
  }

  async withClockAndDir(now, dir, fn) {
    const prevClock = this.clock;
    const prevDir = this.dir;
    this.clock = () => now;
    this.dir = dir;
    try {
      return await fn();
    } finally {
      this.clock = prevClock;
      this.dir = prevDir;
    }
  }

  isClient(device) {
    return this.dir.read('devices', device)?.kind === 'client';
  }

  // A revoke wipes the client's slots, queue and sequence, and ends what is parked for it. The enrollment row stays.
  revokeClient(device) {
    this.sql.exec('DELETE FROM fabric_slots WHERE device = ?', device);
    this.sql.exec('DELETE FROM fabric_versions WHERE device = ?', device);
    this.sql.exec('DELETE FROM fabric_queue WHERE device = ?', device);
    this.sql.exec('DELETE FROM fabric_seq WHERE device = ?', device);
    this.whenRevoked(device);
  }

  async validateRegisterClient({ targetDevice, body, identityHeader }) {
    ensureDevice(targetDevice);
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
    return { inBody, name };
  }

  async registerClient({ caller, targetDevice, body, identityHeader, identity }) {
    this.#requireFullCaller(caller);
    const { inBody, name } = await this.validateRegisterClient({ targetDevice, body, identityHeader });
    if (this.dir.read('devices', targetDevice)) throw exists();
    this.dir.putClientDevice(targetDevice, { V: 1, kind: 'client', methods: inBody.methods, name, added_by: identity });
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
    // The counter goes up by one on every PUT, a rewrite of the same bytes included, and the slot keeps the value it was written at.
    const version = this.#clientVersion(targetDevice) + 1;
    this.sql.exec('INSERT INTO fabric_versions VALUES (?,?) ON CONFLICT(device) DO UPDATE SET version = excluded.version', targetDevice, version);
    this.sql.exec(
      'INSERT OR REPLACE INTO fabric_slots VALUES (?,?,?,?,?,?)',
      targetDevice,
      name,
      base64(sealed),
      caller,
      this.clock(),
      version,
    );
  }

  // `seen` runs once the caller is known to be the client and its own device, before the slot is looked for: a read of a slot that is not there is still the phone looking.
  getSlot({ caller, targetDevice, name, body, seen = () => {} }) {
    ensureDevice(targetDevice);
    ensureName(name);
    this.#requireClientCaller(caller, targetDevice);
    this.#requireTargetClient(targetDevice);
    if (body.length !== 0) throw badRequest();
    seen();
    this.#prune(targetDevice);
    const row = this.sql
      .exec('SELECT sealed, writer, at, version FROM fabric_slots WHERE device = ? AND name = ?', targetDevice, name)
      .toArray()[0];
    if (!row) throw notFound();
    return { sealed: row.sealed, writer: row.writer, at: row.at, version: row.version };
  }

  /**
   * watchSlots is the first look of a slot wait (routes.json slots.watch). It answers {changes} when the client's slot
   * version is past `since`: the version and the names of the slots written after it. Otherwise it answers
   * {since, seconds}, which says nothing newer has arrived and how long the caller asked to wait; the host parks the
   * request, and asks again with `seconds` forced to 0 when the wait ends. The query is read before the caller is
   * judged, as the Go relay reads it, so a malformed one is 400 whoever asks.
   */
  watchSlots({ caller, targetDevice, query, body, seen = () => {} }) {
    const since = parseSince(query.get('since'));
    const seconds = parseWaitSeconds(query.get('wait'));
    ensureDevice(targetDevice);
    this.#requireClientCaller(caller, targetDevice);
    this.#requireTargetClient(targetDevice);
    if (body.length !== 0) throw badRequest();
    seen();
    this.#prune(targetDevice);
    const version = this.#clientVersion(targetDevice);
    if (BigInt(version) <= since) return { since, seconds };
    // A since past 2^63 cannot be a SQLite integer, and no slot can be newer than it.
    const names = since > BigInt(Number.MAX_SAFE_INTEGER)
      ? []
      : this.sql.exec('SELECT name FROM fabric_slots WHERE device = ? AND version > ? ORDER BY name', targetDevice, Number(since)).toArray().map((r) => r.name);
    return { changes: { version, slots: names } };
  }

  /** computers is what a phone may know of the identity's full devices: the id, whether a runner is alive on it, and the time it was last heard from. */
  computers({ caller, targetDevice, body, online, serving }) {
    ensureDevice(targetDevice);
    this.#requireClientCaller(caller, targetDevice);
    this.#requireTargetClient(targetDevice);
    if (body.length !== 0) throw badRequest();
    const now = this.clock();
    const devices = typeof this.dir.devices === 'function' ? this.dir.devices() : this.dir.list().devices;
    const computers = Object.keys(devices)
      .filter((id) => devices[id].kind !== 'client' && devices[id].revoked !== true)
      .sort()
      // The time is the one presence reports: now while the device holds a live socket, else when it was last seen.
      .map((id) => ({ device: id, serving: serving.has(id), at: online.has(id) ? now : (devices[id].last_seen ?? 0) }));
    return { now, computers };
  }

  /** putEnrollment stores the identity's signed statement admitting one phone (routes.json enroll.put). The phone need not be registered yet. */
  async putEnrollment({ caller, targetDevice, body, identityHeader }) {
    this.#requireFullCaller(caller);
    ensureDevice(targetDevice);
    const enrollment = await checkEnrollment(body, identityHeader, targetDevice);
    // A directory row for this id that is a computer, or a revoked phone, is not a phone to admit.
    const row = this.dir.read('devices', targetDevice);
    if (row && (row.kind !== 'client' || row.revoked === true)) throw exists();
    const stored = this.sql.exec('SELECT enroll FROM fabric_enroll WHERE device = ?', targetDevice).toArray()[0];
    if (stored) {
      const before = JSON.parse(stored.enroll);
      if (before.ver > enrollment.ver) throw versionConflict();
      if (before.ver === enrollment.ver) {
        if (!sameEnrollment(before, enrollment)) throw versionConflict();
        return; // the same statement again: a retry after a lost answer is safe
      }
    } else if (this.sql.exec('SELECT COUNT(*) AS n FROM fabric_enroll').one().n >= MAX_ENROLLMENTS) {
      throw full();
    }
    this.sql.exec(
      'INSERT INTO fabric_enroll VALUES (?,?) ON CONFLICT(device) DO UPDATE SET enroll = excluded.enroll',
      targetDevice,
      JSON.stringify(enrollment),
    );
  }

  /** hasEnrollment says whether an enrollment is stored for the device: the precondition of a phone's link approval (link/decide.js). */
  hasEnrollment(device) {
    return this.sql.exec('SELECT 1 FROM fabric_enroll WHERE device = ?', device).toArray().length > 0;
  }

  /** listEnrollments is every stored enrollment, a revoked phone's included, in device order (routes.json enroll.list). */
  listEnrollments({ caller, body }) {
    this.#requireFullCaller(caller);
    if (body.length !== 0) throw badRequest();
    const rows = this.sql.exec('SELECT device, enroll FROM fabric_enroll ORDER BY device').toArray();
    return { clients: rows.map((row) => ({ device: row.device, enroll: JSON.parse(row.enroll) })) };
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
    // A claim may carry a random token that names it. The same caller and token may claim the entry again, so a lost
    // answer is recoverable without a second owner; a claim with no token never matches an owned entry.
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
    // claimed_by is the caller, or caller:token. An ack needs only the owner, whatever token it claimed with.
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

  #clientVersion(device) {
    return this.sql.exec('SELECT version FROM fabric_versions WHERE device = ?', device).toArray()[0]?.version ?? 0;
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

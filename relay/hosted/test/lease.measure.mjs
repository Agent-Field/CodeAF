// Cost of one idle held hour on the Worker's directory, before and after lease liveness from the watch socket
// (contract 21.11): SQLite rows written and requests that reach the object, counted by running the real Directory
// over a SQLite that counts. The client is modelled by its rule: before, a heartbeat every 30 s; after, none while
// its hold socket pings (pings are answered by the platform and never reach this code). Run: node test/lease.measure.mjs
import { DatabaseSync } from 'node:sqlite';
import { Directory } from '../src/directory.js';
import { AMENDED } from '../src/rules.js';
import { Watchers } from '../src/watch.js';
import { init } from './helpers.js';

const HOUR = 3_600_000;
const BEAT = 30_000;
const PING = 30_000;
globalThis.WebSocketRequestResponsePair ??= class {};

/** countingSql is a Durable Object's sql.exec over SQLite that adds up the rows every statement changed. */
function countingSql() {
  const db = new DatabaseSync(':memory:');
  const tally = { rows: 0, statements: 0 };
  return {
    tally,
    exec(query, ...params) {
      const st = db.prepare(query);
      const reads = /^\s*(SELECT|WITH)/i.test(query);
      const rows = reads ? st.all(...params).map((r) => ({ ...r })) : [];
      if (!reads) (tally.rows += Number(st.run(...params).changes), (tally.statements += 1));
      return { toArray: () => rows, one: () => rows[0] };
    },
  };
}

function hour(beats) {
  const clock = { now: 1_000_000 };
  const sql = countingSql();
  const sockets = [{ tag: 'dev_a', pinged: null, deserializeAttachment: () => ({ at: clock.now, holds: [['c1', 1]] }) }];
  const ctx = {
    setWebSocketAutoResponse() {},
    getWebSockets: () => sockets,
    getWebSocketAutoResponseTimestamp: (w) => (w.pinged ? new Date(w.pinged) : null),
  };
  const watchers = new Watchers(ctx, 1000, AMENDED.ttlMs);
  const dir = new Directory(sql, 'id_t', () => clock.now, AMENDED, { broadcast() {}, closeDevice() {}, closeAll() {}, vouchedUntil: (d, c, f) => watchers.vouchedUntil(d, c, f) });
  dir.create('c1', init(), 'dev_a');
  sql.tally.rows = 0;
  let requests = 0;
  let pings = 0;
  for (let t = 0; t < HOUR; t += 5_000) {
    clock.now += 5_000;
    if ((t + 5_000) % PING === 0) (sockets[0].pinged = clock.now, (pings += 1));
    if (beats && (t + 5_000) % BEAT === 0) (dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 }), (requests += 1));
  }
  const held = dir.cell('c1').cell.lease.expires > clock.now;
  return { requests, pings, rows: sql.tally.rows, held };
}

const before = hour(true);
const after = hour(false);
console.log(JSON.stringify({ before, after }, null, 1));
if (!after.held || after.rows !== 0 || after.requests !== 0) process.exit(1);

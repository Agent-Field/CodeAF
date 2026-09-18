// schema.js validates the submit wire. The Go client writes exactly these
// rows (internal/pool/outbox.Row carrying internal/pool/record.Row), so this
// module refuses anything the client would not have sent.
//
// One NDJSON line is an outer envelope plus the payload it carries:
//   {"schema":1,"day":"YYYY-MM-DD","nonce":"<32 lowercase hex>","payload":{...}}
// The payload is the record.Row fields:
//   {"schema":1,"metric":"role_quality","role":...,"model":...,"score":...,
//    "judge":...,"door":...,"size":...,"day":"YYYY-MM-DD"}
//
// Everything here is pure: no KV, no fetch, no clock of its own.

// The one metric the client records.
const METRIC = 'role_quality';

const ROLES = new Set(['worker', 'high', 'mastermind']);
const DOORS = new Set(['task', 'do']);
const SIZES = new Set(['S', 'M', 'L']);

// A model id is "<vendor>/<id>". The Go side draws both halves from a closed
// set of provider and model names; this is the loose shape it always matches.
const MODEL_RE = /^[a-z0-9._-]+\/[a-z0-9._:-]+$/i;
const DAY_RE = /^\d{4}-\d{2}-\d{2}$/;
const HEX32_RE = /^[0-9a-f]{32}$/;

// A row's day may be at most one day ahead of now (UTC): a clock a little off
// is fine, a date in the future is not.
const ONE_DAY_MS = 86_400_000;

// utcDay reads a "YYYY-MM-DD" day as milliseconds since the epoch, UTC.
function utcDay(day) {
  const y = Number(day.slice(0, 4));
  const m = Number(day.slice(5, 7));
  const d = Number(day.slice(8, 10));
  return Date.UTC(y, m - 1, d);
}

// dayError answers why a day string is unusable, or null when it is fine.
function dayError(day, now) {
  if (typeof day !== 'string' || !DAY_RE.test(day)) {
    return 'day must be YYYY-MM-DD';
  }
  const todayMs = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  if (utcDay(day) - todayMs > ONE_DAY_MS) {
    return 'day is more than one day in the future';
  }
  return null;
}

// validateRow reads one NDJSON line. It answers {row, error}: row is the
// parsed envelope with its payload when the line is good, and null with a
// message otherwise. `now` bounds how far ahead the day may be.
export function validateRow(line, now = new Date()) {
  if (typeof line !== 'string' || line.trim() === '') {
    return fail('line is empty');
  }
  let outer;
  try {
    outer = JSON.parse(line);
  } catch {
    return fail('line is not json');
  }
  if (outer === null || typeof outer !== 'object' || Array.isArray(outer)) {
    return fail('row is not an object');
  }
  if (outer.schema !== 1) {
    return fail('schema must be 1');
  }
  const dayBad = dayError(outer.day, now);
  if (dayBad !== null) {
    return fail(dayBad);
  }
  if (typeof outer.nonce !== 'string' || !HEX32_RE.test(outer.nonce)) {
    return fail('nonce must be 32 lowercase hex');
  }
  const payload = outer.payload;
  if (payload === null || typeof payload !== 'object' || Array.isArray(payload)) {
    return fail('payload is not an object');
  }
  if (payload.schema !== 1) {
    return fail('payload schema must be 1');
  }
  if (payload.metric !== METRIC) {
    return fail(`metric must be ${METRIC}`);
  }
  if (!ROLES.has(payload.role)) {
    return fail('role must be worker, high or mastermind');
  }
  if (typeof payload.model !== 'string' || !MODEL_RE.test(payload.model)) {
    return fail('model must be <vendor>/<id>');
  }
  if (typeof payload.score !== 'number' || !Number.isFinite(payload.score)
      || payload.score < 0 || payload.score > 100) {
    return fail('score must be a number in 0-100');
  }
  if (typeof payload.judge !== 'string' || !MODEL_RE.test(payload.judge)) {
    return fail('judge must be <vendor>/<id>');
  }
  if (!DOORS.has(payload.door)) {
    return fail('door must be task or do');
  }
  if (!SIZES.has(payload.size)) {
    return fail('size must be S, M or L');
  }
  const payloadDayBad = dayError(payload.day, now);
  if (payloadDayBad !== null) {
    return fail(payloadDayBad);
  }
  return {
    row: {
      schema: 1,
      day: outer.day,
      nonce: outer.nonce,
      payload: {
        schema: 1,
        metric: payload.metric,
        role: payload.role,
        model: payload.model,
        score: payload.score,
        judge: payload.judge,
        door: payload.door,
        size: payload.size,
        day: payload.day,
      },
    },
    error: null,
  };
}

// validateInstall answers whether a request's X-Codeaf-Install header is an
// install id: 32 lowercase hex characters, the install's own nonce.
export function validateInstall(header) {
  return typeof header === 'string' && HEX32_RE.test(header);
}

function fail(message) {
  return { row: null, error: message };
}

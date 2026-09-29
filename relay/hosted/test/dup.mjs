// Measures the "in-flight put" window against `wrangler dev --local`: the client sends a whole
// frame, hangs up before the answer, and asks Has at once. A "no" here would make a client
// resend a frame that is about to land. Not part of `npm test`.
import http from 'node:http';
import { newIdentity, newDevice, signed } from './party.js';
import { frame, rid } from './helpers.js';

const [host, port] = (process.env.RELAY ?? 'http://127.0.0.1:18791').replace('http://', '').split(':');
const TRIALS = Number(process.env.TRIALS ?? 100);
const KB = Number(process.env.KB ?? 32);
const dev = await newDevice(await newIdentity());
const enc = new TextEncoder();

function hangUp(path, bytes) {
  return signed(dev, 'POST', path, bytes).then((h) => new Promise((done) => {
    const req = http.request({ host, port, method: 'POST', path, headers: { ...h, 'content-length': bytes.length } });
    req.on('error', () => {});
    req.write(bytes, () => (req.destroy(), done())); // fully written, answer never read
  }));
}
async function has(rids) {
  const body = enc.encode(JSON.stringify({ rids }));
  const res = await fetch(`http://${host}:${port}/v1/store/has`, { method: 'POST', headers: await signed(dev, 'POST', '/v1/store/has', body), body });
  return (await res.json()).have;
}

let noFirst = 0, noAfter3 = 0;
for (let i = 0; i < TRIALS; i++) {
  const r = rid(1_000_000 + i);
  const f = frame([r], 'x'.repeat(KB * 1024));
  await hangUp('/v1/store/frames', f.bytes);
  const asks = [];
  for (let k = 0; k < 3; k++) asks.push((await has([r]))[0]);
  if (!asks[0]) noFirst++;
  if (!asks.some(Boolean)) noAfter3++;
}
console.log(JSON.stringify({ trials: TRIALS, frame_kb: KB, first_has_said_no: noFirst, all_three_said_no: noAfter3 }));

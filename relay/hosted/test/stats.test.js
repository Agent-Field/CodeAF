import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Meta } from '../src/meta.js';
import { Stats } from '../src/stats.js';
import { Counters } from '../src/counters.js';
import { memorySql } from './sql.js';

test('counts are batched: one flush request however many changes, and one write when flushed', () => {
  const sql = memorySql();
  let scheduled = 0;
  const stats = new Stats(new Meta(sql), () => scheduled++);
  stats.add({ puts: 1, bytes_in: 10 });
  stats.add({ gets: 2 });
  stats.add({ puts: 1 });
  assert.equal(scheduled, 1);
  assert.deepEqual(stats.snapshot(), { puts: 2, gets: 2, has: 0, bytes_in: 10, bytes_out: 0 });
  stats.flush();
  stats.add({ has: 1 });
  assert.equal(scheduled, 2, 'a change after a flush asks for the next one');
});

test('flushed counts survive the object: a new one over the same storage carries on from them', () => {
  const sql = memorySql();
  const first = new Stats(new Meta(sql), () => {});
  first.add({ puts: 3, bytes_out: 7 });
  first.flush();
  const second = new Stats(new Meta(sql), () => {});
  assert.deepEqual(second.snapshot(), { puts: 3, gets: 0, has: 0, bytes_in: 0, bytes_out: 7 });
});

test('a flush with nothing changed writes nothing', () => {
  const meta = new Meta(memorySql());
  let writes = 0;
  const set = meta.set.bind(meta);
  meta.set = (...a) => (writes++, set(...a));
  const stats = new Stats(meta, () => {});
  stats.flush();
  assert.equal(writes, 0);
});

test('counters count per key inside a window, report the wait, and start over after it', () => {
  const c = new Counters(memorySql());
  assert.deepEqual(c.hit('ip', 1000, 0), { n: 1, retryAfter: 1 });
  assert.equal(c.hit('ip', 1000, 400).n, 2);
  assert.equal(c.hit('other', 1000, 400).n, 1);
  assert.equal(c.hit('ip', 1000, 1000).n, 1);
});

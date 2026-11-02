import { test } from 'node:test';
import assert from 'node:assert/strict';
import { matchRoute } from '../src/routes.js';
import { Directory } from '../src/directory.js';
import { memorySql } from './sql.js';
import { init } from './helpers.js';
import { AMENDED } from '../src/rules.js';

// C7: the real cell-read route promises conditional takes before the client moves a chat.
test('C7 the Worker states conditional takes on the cell read', async () => {
  const dir = new Directory(memorySql(), 'id_c7', () => 1_000_000, AMENDED);
  dir.create('c7', init(), 'dev_a');
  const route = matchRoute('GET', '/v1/dir/cells/c7');
  const response = await route.handler({ tenant: { dir } }, route.args);
  assert.equal(response.headers.get('codeaf-conditional-takes'), '1');
  const { cell } = await response.json();
  assert.throws(() => dir.acquire('c7', 'dev_b', true, { device: 'someone_else', fence: cell.lease.fence }), (e) => e.code === 'lease_held');
  assert.equal(dir.cell('c7').cell.lease.device, 'dev_a');
});

test('F2 a same-holder fence bump is harmless only with the same saved head', () => {
  for (const change of ['fence', 'head', 'holder']) {
    const dir = new Directory(memorySql(), 'id_f2', () => 1_000_000, AMENDED);
    dir.create('f2', init(), 'dev_a');
    const original = dir.cell('f2').cell;
    const seen = { device: original.lease.device, fence: original.lease.fence, head: original.head };
    dir.acquire('f2', change === 'holder' ? 'dev_c' : 'dev_a', true);
    if (change === 'head') {
      const current = dir.cell('f2').cell;
      dir.publish('f2', 'dev_a', { fence: current.lease.fence, old_head: current.head, head: 'new-head' });
    }
    if (change === 'fence') assert.equal(dir.acquire('f2', 'dev_b', true, seen).cell.lease.device, 'dev_b');
    else assert.throws(() => dir.acquire('f2', 'dev_b', true, seen), (e) => e.code === 'lease_held');
  }
});

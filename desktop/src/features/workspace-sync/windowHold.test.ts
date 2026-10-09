import test from 'node:test';
import assert from 'node:assert/strict';
import { holdCurrentWindow } from './windowStore.ts';

test('a place remount keeps this page window identity while the previous strip releases its lock', async context => {
 context.mock.timers.enable({ apis: ['setTimeout'] });
 let requests = 0;
 let releases = 0;
 const locks = { request: async (_name: string, _options: { ifAvailable?: boolean }, run: (lock: unknown) => Promise<void> | void) => { requests++; await run({}); releases++; } };
 const first = await holdCurrentWindow('window-place-handover-test', locks);
 assert.equal(first.held, true);
 first.release();
 const next = await holdCurrentWindow('window-place-handover-test', locks);
 context.mock.timers.tick(0);
 assert.equal(next.held, true);
 assert.equal(requests, 1, 'same page reuses ownership rather than treating itself as another window');
 assert.equal(releases, 0);
 next.release();
 context.mock.timers.tick(0);
 await Promise.resolve();
 assert.equal(releases, 1, 'leaving the page releases the real window lock once');
});

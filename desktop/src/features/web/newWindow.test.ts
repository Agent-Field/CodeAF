import test from 'node:test';
import assert from 'node:assert/strict';
import { desktopTabEvent } from '../../lib/desktopMenuRoute.ts';
import { initialWorkspace, workspaceReducer } from '../tabs/model.ts';
import { newTab } from '../tabs/helpers.ts';
import { isOpenWebDetail, webOpenAction } from './open.ts';
import { acceptPageNewWindow, NEW_WINDOW_BURST, NEW_WINDOW_BURST_MS, type NewWindowGate } from './newWindow.ts';

function installWindow(): { seen: unknown[]; restore: () => void } {
  const target = new EventTarget();
  const seen: unknown[] = [];
  target.addEventListener(desktopTabEvent, event => seen.push((event as CustomEvent).detail));
  const previous = globalThis.window;
  globalThis.window = { dispatchEvent: (event: Event) => target.dispatchEvent(event) } as unknown as Window & typeof globalThis;
  return { seen, restore: () => { globalThis.window = previous; } };
}

const gate = (): NewWindowGate => ({ openedAt: new Map() });

test('a target=_blank request adds one background tab after the opener', () => {
  const { seen, restore } = installWindow();
  try {
    const opener = newTab({ id: 'page', kind: 'web', title: 'pkg.go.dev', target: { url: 'https://pkg.go.dev' } });
    const neighbour = newTab({ id: 'chat' });
    const state = { ...initialWorkspace(), tabs: [opener, neighbour], activeId: 'page' };
    const tab = acceptPageNewWindow({ opener: 'page', url: 'https://go.dev/play' }, 0, gate());
    assert.ok(tab);
    assert.equal(tab.kind, 'web');
    assert.equal(tab.web?.url, 'https://go.dev/play');
    assert.equal(seen.length, 1);
    const detail = seen[0];
    assert.ok(isOpenWebDetail(detail));
    assert.equal(detail.background, true);
    assert.equal(detail.afterId, 'page');
    const action = webOpenAction(state.tabs, detail);
    assert.equal(action.at, 1);
    assert.equal(action.background, true);
    const next = workspaceReducer(state, action);
    assert.deepEqual(next.tabs.map(t => t.id), ['page', tab.id, 'chat']);
    assert.equal(next.activeId, 'page');
  } finally { restore(); }
});

test('a non-http address is dropped and does not spend a burst slot', () => {
  const { seen, restore } = installWindow();
  try {
    const fresh = gate();
    for (const url of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,x', 'about:blank', '']) {
      assert.equal(acceptPageNewWindow({ opener: 'page', url }, 0, fresh), null, url);
    }
    assert.equal(acceptPageNewWindow({ opener: '', url: 'https://go.dev' }, 0, fresh), null);
    assert.equal(seen.length, 0);
    assert.ok(acceptPageNewWindow({ opener: 'page', url: 'https://go.dev' }, 0, fresh));
    assert.equal(seen.length, 1);
  } finally { restore(); }
});

test('more than five opens from one page inside two seconds stay at five', () => {
  const { seen, restore } = installWindow();
  try {
    const fresh = gate();
    const t = 5_000;
    for (let i = 0; i < NEW_WINDOW_BURST; i++) assert.ok(acceptPageNewWindow({ opener: 'page', url: `https://ex.dev/${i}` }, t, fresh));
    assert.equal(acceptPageNewWindow({ opener: 'page', url: 'https://ex.dev/more' }, t, fresh), null);
    assert.equal(acceptPageNewWindow({ opener: 'page', url: 'https://ex.dev/edge' }, t + NEW_WINDOW_BURST_MS, fresh), null);
    assert.ok(acceptPageNewWindow({ opener: 'other', url: 'https://ex.dev/other' }, t, fresh));
    assert.ok(acceptPageNewWindow({ opener: 'page', url: 'https://ex.dev/later' }, t + NEW_WINDOW_BURST_MS + 1, fresh));
    assert.equal(seen.length, NEW_WINDOW_BURST + 2);
    for (const detail of seen) {
      assert.ok(isOpenWebDetail(detail));
      assert.equal(detail.background, true);
    }
  } finally { restore(); }
});

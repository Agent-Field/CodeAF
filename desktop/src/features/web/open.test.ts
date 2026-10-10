import test from 'node:test';
import assert from 'node:assert/strict';
import { desktopTabEvent } from '../../lib/desktopMenuRoute.ts';
import { initialWorkspace, workspaceReducer } from '../tabs/model.ts';
import { newTab } from '../tabs/helpers.ts';
import { isOpenWebDetail, openWebDetail, openWebTab, webOpenAction } from './open.ts';

function installWindow(): { seen: unknown[]; restore: () => void } {
  const target = new EventTarget();
  const seen: unknown[] = [];
  target.addEventListener(desktopTabEvent, event => seen.push((event as CustomEvent).detail));
  const previous = globalThis.window;
  globalThis.window = { dispatchEvent: (event: Event) => target.dispatchEvent(event) } as unknown as Window & typeof globalThis;
  return { seen, restore: () => { globalThis.window = previous; } };
}

test('refuses javascript:, places after afterId, background keeps focus', () => {
  const { seen, restore } = installWindow();
  try {
    for (const bad of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,x', 'what is go']) assert.equal(openWebTab(bad), null, bad);
    assert.equal(seen.length, 0);

    const a = newTab({ id: 'a' });
    const b = newTab({ id: 'b' });
    const state = { ...initialWorkspace(), tabs: [a, b], activeId: 'a' };
    const tab = openWebTab('www.pkg.go.dev/fmt', { afterId: 'a', background: true });
    assert.ok(tab);
    assert.equal(tab.kind, 'web');
    assert.equal(tab.web?.url, 'https://www.pkg.go.dev/fmt');
    assert.equal(tab.title, 'pkg.go.dev');
    const detail = seen[0];
    assert.ok(isOpenWebDetail(detail));
    const action = webOpenAction(state.tabs, detail);
    assert.equal(action.at, 1);
    const next = workspaceReducer(state, action);
    assert.deepEqual(next.tabs.map(t => t.id), ['a', tab.id, 'b']);
    assert.equal(next.activeId, 'a');

    const front = workspaceReducer(state, webOpenAction(state.tabs, openWebDetail('https://x.com', { afterId: 'a' })!));
    assert.equal(front.activeId, front.tabs[1].id);
    assert.equal(webOpenAction(state.tabs, openWebDetail('https://x.com', { afterId: 'gone' })!).at, undefined);
  } finally { restore(); }
});

test('a menu command is not a web-open detail', () => {
  assert.equal(isOpenWebDetail('new'), false);
});

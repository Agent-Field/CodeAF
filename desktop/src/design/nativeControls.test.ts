import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import {
  attentionItem,
  createNativeControls,
  handoffFromPane,
  handoffProblem,
  isPlaceKey,
  isSafeWebUrl,
  needsYouCount,
  NOTICE_ACTIVATED_EVENT,
  noticeTarget,
  paneFromHandoff,
  placeFromSearch,
  type NativeBridge,
  type TabHandoff,
} from './nativeControls.ts';
import type { Pane } from '../features/tabs/types.ts';

type Call = { command: string; args?: Record<string, unknown> };

/** A fake Tauri: records every invoke, answers from a table, and lets a test fire window events. */
function fakeBridge(answers: Record<string, unknown> = {}, desktop = true) {
  const calls: Call[] = [];
  const handlers = new Map<string, (payload: unknown) => void>();
  const opened: string[] = [];
  const bridge: NativeBridge = {
    desktop,
    async invoke<T>(command: string, args?: Record<string, unknown>) {
      calls.push({ command, args });
      const answer = answers[command];
      if (answer instanceof Error) throw answer;
      return (typeof answer === 'function' ? answer(args) : answer) as T;
    },
    async listen<T>(event: string, handler: (payload: T) => void) {
      handlers.set(event, handler as (payload: unknown) => void);
      return () => handlers.delete(event);
    },
    location: { pathname: '/index.html', search: '?place=pl_00000000000000ab' },
    openBrowserTab: url => opened.push(url),
  };
  return { bridge, calls, fire: (event: string, payload: unknown) => handlers.get(event)?.(payload), opened };
}

const pane = (extra: Partial<Pane> = {}): Pane => ({
  id: 'tab-7',
  kind: 'conversation',
  title: 'Fix the parser',
  draft: 'half a thought',
  sessionFile: '/home/me/.codeaf/v3/projects/x/abc/session.jsonl',
  route: { taskId: 't1', back: [''], forward: [] },
  folded: { 'turn-1': true },
  tasksClosed: true,
  target: { sessionId: 'abc', url: 'https://example.com/a', shot: 'data:image/png;base64,AAAA' },
  ...extra,
});

test('renderer place keys follow the current graph until the window contract is reconciled (WIN-46)', () => {
  for (const good of ['now', 'root', 'pl_0123456789abcdef']) assert.ok(isPlaceKey(good), good);
  for (const bad of ['', 'Now', 'pl_', 'pl_0123456789ABCDEF', 'pl_0123456789abcde', 'p-0123456789ab', 'now&token=x', '../now', 7, null])
    assert.ok(!isPlaceKey(bad), String(bad));
});

test('a window reads its place from its own address, falling back to now', () => {
  assert.equal(placeFromSearch('?place=pl_0123456789abcdef'), 'pl_0123456789abcdef');
  assert.equal(placeFromSearch('?place=root'), 'root');
  assert.equal(placeFromSearch('?place=../../etc'), 'now');
  assert.equal(placeFromSearch(''), 'now');
});

test('a handoff carries the tab and leaves window-local state and the screenshot behind', () => {
  const tab = handoffFromPane(pane());
  assert.deepEqual(tab, {
    kind: 'conversation',
    title: 'Fix the parser',
    draft: 'half a thought',
    sessionFile: '/home/me/.codeaf/v3/projects/x/abc/session.jsonl',
    route: { taskId: 't1', back: [''], forward: [] },
    target: { sessionId: 'abc', url: 'https://example.com/a' },
  });
  assert.equal(handoffProblem(tab), undefined);
  const back = paneFromHandoff(tab, 'tab-1');
  assert.equal(back.id, 'tab-1');
  assert.equal(back.draft, 'half a thought');
  assert.equal(back.sessionFile, tab.sessionFile);
  assert.deepEqual(back.route, tab.route);
  assert.equal(back.folded, undefined);
});

test('a handoff with credentials, a strange scheme or oversized fields is refused before Rust sees it', () => {
  const base = handoffFromPane(pane());
  const bad: TabHandoff[] = [
    { ...base, target: { url: 'https://user:pw@example.com' } },
    { ...base, target: { url: 'file:///etc/passwd' } },
    { ...base, target: { url: 'javascript:alert(1)' } },
    { ...base, title: 'x'.repeat(201) },
    { ...base, title: 'bell\u0007' },
    { ...base, sessionFile: 'relative/session.jsonl' },
    { ...base, draft: 'x'.repeat(64 * 1024 + 1) },
    { ...base, route: { back: Array(65).fill(''), forward: [] } },
    { ...base, kind: 'shell' as never },
  ];
  for (const tab of bad) assert.ok(handoffProblem(tab), JSON.stringify(tab).slice(0, 80));
  assert.ok(isSafeWebUrl('http://localhost:3000/x'));
  assert.ok(!isSafeWebUrl('https:///nohost'));
});

test('moving a tab: the source removes it only after the target claims it', async () => {
  const { bridge, calls, fire } = fakeBridge({ window_open: { label: 'w-2', handoffId: 'h-1' } });
  const native = createNativeControls(bridge);
  const removed: string[] = [];
  await native.onHandoffClaimed(id => removed.push(id));
  const result = await native.openPlaceWindow('pl_0123456789abcdef', { pane: pane(), at: { x: 40, y: 50 } });
  assert.deepEqual(result, { label: 'w-2', handoffId: 'h-1', moved: true });
  assert.equal(calls[0].command, 'window_open');
  const request = calls[0].args?.request as Record<string, unknown>;
  assert.equal(request.placeKey, 'pl_0123456789abcdef');
  assert.deepEqual(request.at, { x: 40, y: 50 });
  assert.ok(!JSON.stringify(request).includes('shot'));
  assert.deepEqual(removed, [], 'nothing leaves before the claim');
  fire('window://handoff-claimed', { handoffId: 'h-other' });
  assert.deepEqual(removed, [], 'a handoff this window did not start is ignored');
  fire('window://handoff-claimed', { handoffId: 'h-1' });
  fire('window://handoff-claimed', { handoffId: 'h-1' });
  assert.deepEqual(removed, ['tab-7'], 'removed once');
});

test('opening a window accepts the native label without inventing a handoff', async () => {
  const { bridge, calls } = fakeBridge({ window_open: 'w-2' });
  const result = await createNativeControls(bridge).openPlaceWindow('now', { focusTab: 'target' });
  assert.deepEqual(result, { label: 'w-2', handoffId: undefined, moved: false });
  assert.deepEqual(calls[0], { command: 'window_open', args: { request: { placeKey: 'now', focusTab: 'target' } } });
});

test('moving into an open window and claiming there', async () => {
  const claimed = { handoffId: 'h-3', ...handoffFromPane(pane()) };
  const { bridge, calls, fire } = fakeBridge({ window_move_tab: { handoffId: 'h-3' }, window_claim_handoff: claimed });
  const native = createNativeControls(bridge);
  let ready = 0;
  await native.onHandoffReady(() => ready++);
  await native.moveTabToWindow('w-4', pane());
  assert.deepEqual((calls[0].args?.request as { to: string }).to, 'w-4');
  fire('window://handoff-ready', { handoffId: 'h-3' });
  assert.equal(ready, 1);
  assert.deepEqual(await native.claimHandoff(), claimed);
});

test('an empty claim is undefined, not an error', async () => {
  const { bridge } = fakeBridge({ window_claim_handoff: null });
  assert.equal(await createNativeControls(bridge).claimHandoff(), undefined);
});

test('an invalid place never reaches Rust', async () => {
  const { bridge, calls } = fakeBridge();
  await assert.rejects(createNativeControls(bridge).openPlaceWindow('p-123' as never), /not a place/);
  assert.equal(calls.length, 0);
});

test('in a browser a window opens as a browser tab on the place and nothing is moved', async () => {
  const { bridge, calls, opened } = fakeBridge({}, false);
  const native = createNativeControls(bridge);
  assert.deepEqual(await native.openPlaceWindow('root', { pane: pane() }), { moved: false });
  assert.deepEqual(opened, ['/index.html?place=root']);
  assert.deepEqual(await native.currentWindow(), { label: 'main', placeKey: 'pl_00000000000000ab' });
  assert.deepEqual(await native.listWindows(), []);
  await assert.rejects(native.moveTabToWindow('w-2', pane()), /desktop app/);
  assert.equal(calls.length, 0);
});

test('choosers: picked, cancelled, busy, and unavailable outside the app', async () => {
  const picked = { status: 'picked', paths: [{ path: '/Users/me/code/app', name: 'app' }] };
  const { bridge, calls } = fakeBridge({ dialog_pick: picked });
  const native = createNativeControls(bridge);
  assert.deepEqual(await native.pickFolder(), picked);
  assert.deepEqual(calls[0].args, { request: { kind: 'folder' } });
  await native.pickFiles({ multiple: true, title: 'Attach' });
  assert.deepEqual(calls[1].args, { request: { kind: 'file', multiple: true, title: 'Attach' } });

  for (const [answer, expected] of [
    [{ status: 'cancelled' }, { status: 'cancelled' }],
    [{ status: 'busy' }, { status: 'busy' }],
    [{ status: 'picked', paths: [] }, { status: 'cancelled' }],
    [{ status: 'picked', paths: [{ path: '', name: '' }, { nope: 1 }] }, { status: 'cancelled' }],
    [{ status: 'surprise' }, { status: 'cancelled' }],
  ] as const) {
    const fake = fakeBridge({ dialog_pick: answer });
    assert.deepEqual(await createNativeControls(fake.bridge).pickFolder(), expected);
  }
  assert.deepEqual(await createNativeControls(fakeBridge({}, false).bridge).pickFolder(), { status: 'unavailable' });
});

test('notifications: the full list goes to Rust; outside the app nothing is claimed to be posted', async () => {
  const items = [attentionItem({ kind: 'needsYou', chatId: 'c1', chatTitle: 'Launch', placeId: 'pl_00000000000000aa', placeName: 'Marketing', text: 'Which branch?' })];
  const { bridge, calls } = fakeBridge({ notify_attention: { posted: 1, groups: 1, skipped: null }, notify_permission: { state: 'granted', verified: false } });
  const native = createNativeControls(bridge);
  assert.deepEqual(await native.notifyAttention(items, 42, 'engine-a'), { posted: 1, groups: 1, skipped: null });
  assert.deepEqual(calls[0].args, { items, seq: 42, epoch: 'engine-a' }, 'the list names the feed reading it came from');
  await native.notifyAttention(items, -1);
  assert.equal(calls[1].args?.seq, 0, 'a nonsense sequence is the oldest reading, never a newer one');
  assert.deepEqual(await native.notificationPermission(), { state: 'granted', verified: false });

  const browser = createNativeControls(fakeBridge({}, false).bridge);
  assert.deepEqual(await browser.notifyAttention(items, 1), { posted: 0, groups: 0, skipped: 'unavailable' });
  assert.deepEqual(await browser.notificationPermission(), { state: 'unavailable', verified: false });
});

test('a notification names only a conversation and question Rust will accept', () => {
  assert.deepEqual(noticeTarget('9446cc2627f3deae', { kind: 'consent', id: 7 }), { chatId: '9446cc2627f3deae', question: { kind: 'consent', id: 7 } });
  // A question the tray cannot match (no engine id) still opens its conversation.
  assert.deepEqual(noticeTarget('s1', { kind: 'consent' }), { chatId: 's1' });
  assert.deepEqual(noticeTarget('s1', { kind: 'consent', id: 0 }), { chatId: 's1' });
  assert.deepEqual(noticeTarget('s1', { kind: 'two words', id: 7 }), { chatId: 's1' });
  assert.deepEqual(noticeTarget('s1', { kind: 'consent', id: 2 ** 53 }), { chatId: 's1' });
  // Rust refuses the whole list over one bad item, so a conversation id that does not fit is left off entirely.
  assert.deepEqual(noticeTarget('../../etc', { kind: 'consent', id: 7 }), {});
  assert.deepEqual(noticeTarget('x'.repeat(129)), {});
  assert.deepEqual(noticeTarget(undefined), {});
});

test('notification clicks: claimed from Rust for this window only, in the one shape a click can carry', async () => {
  const { bridge, calls, fire } = fakeBridge({ notify_claim: [
    { chatId: 's1', question: { kind: 'consent', id: 7 } },
    { chatId: 's2' },
    { chatId: '../x' },
    { chatId: 's3', question: { kind: 'consent', id: -1 } },
    'codeaf://chat/s4',
    null,
  ] });
  const native = createNativeControls(bridge);
  let heard = 0;
  const off = await native.onNoticeActivated(() => heard++);
  assert.equal(NOTICE_ACTIVATED_EVENT, 'notification://activated');
  fire(NOTICE_ACTIVATED_EVENT, undefined);
  assert.equal(heard, 1);
  assert.deepEqual(await native.claimNotices(), [{ chatId: 's1', question: { kind: 'consent', id: 7 } }, { chatId: 's2' }]);
  assert.deepEqual(calls.map(c => c.command), ['notify_claim']);
  off();

  const browser = createNativeControls(fakeBridge({}, false).bridge);
  assert.deepEqual(await browser.claimNotices(), []);
});

test('the badge is the needs-you count, clamped, and unavailable in a browser', async () => {
  const items = [
    attentionItem({ kind: 'needsYou', chatId: 'a', chatTitle: '', text: '' }),
    attentionItem({ kind: 'needsYou', chatId: 'a', chatTitle: '', text: '' }),
    attentionItem({ kind: 'needsYou', chatId: 'b', chatTitle: '', text: '' }),
    attentionItem({ kind: 'failed', chatId: 'c', chatTitle: '', text: '' }),
    attentionItem({ kind: 'running', chatId: 'd', chatTitle: '', text: '', taskId: 't1' }),
  ];
  assert.equal(needsYouCount(items), 2);
  const { bridge, calls } = fakeBridge({ badge_set: { applied: true } });
  const native = createNativeControls(bridge);
  await native.setBadge(-3, 7);
  await native.setBadge(1e9, 7);
  await native.setBadge(Number.NaN, 1.5);
  assert.deepEqual(calls.map(c => c.args?.count), [0, 9999, 0]);
  assert.deepEqual(calls.map(c => c.args?.seq), [7, 7, 0]);
  assert.deepEqual(await createNativeControls(fakeBridge({}, false).bridge).setBadge(2, 1), { applied: false, reason: 'unavailable' });
});

test('places-routes attention rows map to items; virtual places are not groups', () => {
  assert.deepEqual(attentionItem({ kind: 'running', chatId: 'c', chatTitle: 'x', text: 'y', taskId: 't9', placeId: 'now', placeName: 'Now' }), {
    id: 'c:t9', kind: 'running', chatTitle: 'x', text: 'y',
  });
});

// ---------------------------------------------------------------------------
// The native contract on disk: every command this adapter calls is registered,
// and the capability reaches codeaf's own windows and nothing else.

const tauriDir = new URL('../../src-tauri/', import.meta.url);
const read = (path: string) => readFileSync(new URL(path, tauriDir), 'utf8');

test('every command the adapter invokes is a registered Rust handler', () => {
  const adapter = readFileSync(new URL('./nativeControls.ts', import.meta.url), 'utf8');
  const invoked = new Set([...adapter.matchAll(/invoke<[^>]*>\('([a-z_]+)'/g), ...adapter.matchAll(/invoke\('([a-z_]+)'/g), ...adapter.matchAll(/'((?:notify|window|dialog|badge)_[a-z_]+)'/g)].map(m => m[1]));
  const lib = read('src/lib.rs');
  for (const command of invoked) assert.match(lib, new RegExp(`::${command}\\b`), `${command} is registered in lib.rs`);
  assert.ok(invoked.size >= 12, `found ${invoked.size} commands`);
});

test('the capability reaches main and w-* only, never a web view, with the reviewed grants and no shell', () => {
  // dialog:allow-open, notification:default, and http:default are on the
  // reviewed allow-list. The http grant is the scoped loopback engine
  // transport restored in capabilities/default.json; shell and filesystem
  // grants stay off, and a web page view is never named.
  const reviewed = [
    'core:default',
    'core:window:allow-start-dragging',
    'core:window:allow-toggle-maximize',
    'core:window:allow-set-theme',
    'core:window:allow-set-title',
    'core:window:allow-set-badge-count',
    'core:webview:allow-set-webview-position',
    'dialog:allow-open',
    'notification:default',
    'http:default',
  ];
  assert.deepEqual((JSON.parse(read('capabilities/default.json')) as { windows: string[] }).windows, ['main', 'w-*']);
  for (const file of readdirSync(new URL('capabilities/', tauriDir))) {
    const capability = JSON.parse(read(`capabilities/${file}`)) as { windows?: string[]; webviews?: string[]; remote?: unknown; permissions: unknown[] };
    for (const label of [...(capability.windows ?? []), ...(capability.webviews ?? [])]) assert.ok(label === 'main' || label === 'w-*', `${file}: ${label}`);
    assert.equal(capability.remote, undefined, `${file} grants no remote origin`);
    const grants = capability.permissions.map(p => (typeof p === 'string' ? p : (p as { identifier: string }).identifier));
    for (const grant of grants) assert.ok(!/^(shell|fs):/.test(grant), `${file}: ${grant} is not a reviewed grant`);
    if (file === 'default.json') assert.deepEqual(grants, reviewed);
  }
});

test('the CSP gains nothing for windows, choosers or notifications', () => {
  const csp = (JSON.parse(read('tauri.conf.json')) as { app: { security: { csp: string } } }).app.security.csp;
  // Native web snapshots already use blob images; window commands add no source.
  assert.equal(
    csp,
    "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' asset: data: blob:; media-src 'self' data:; connect-src ipc: http://ipc.localhost http://127.0.0.1:*",
  );
});

test('the window leaves drag and drop to the page, so tabs can be dragged into a split', () => {
  // On macOS the native drop handler swallows every HTML5 drag inside the view. Tab reordering,
  // tear-off and the split edge zones are HTML5 drags, and no code listens for native drops.
  const windows = (JSON.parse(read('tauri.conf.json')) as { app: { windows: { dragDropEnabled?: boolean }[] } }).app.windows;
  for (const window of windows) assert.equal(window.dragDropEnabled, false);
});

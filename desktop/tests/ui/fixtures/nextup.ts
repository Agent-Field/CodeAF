import { expect, type Page } from '@playwright/test';
import type { EngineAnswer, EngineSnapshot } from '../../../src/features/chat/engine-client';
import type { AttentionItem, WorldRow } from '../../../src/features/chat/world-client';
import { installMockEngine } from '../support/mock-engine';
import { installMockPlaces, sessionFileFor } from '../support/mock-places';
import { pendingQuestion } from '../support/scenarios';

export const origin = '1111111111111111';
const places = [
 { id: 'pl_0000000000000001', name: 'Marketing', tint: 'rose' as const, pinned: true },
 { id: 'pl_0000000000000002', name: 'Software', tint: 'tide' as const, pinned: true },
 { id: 'pl_0000000000000003', name: 'Ledger', tint: 'sand' as const, pinned: true },
];

// The feed deliberately puts a nonblocking suggestion and a delete before the blockers.
// The browser must arrange these engine facts, rather than inherit the fixture's order.
export async function openNextUp(page: Page, theme: 'light' | 'dark', options: { arrival?: boolean } = {}) {
 const engine = await installMockEngine(page, pendingQuestion());
 const question = engine.snapshot().questions![0];
 const definitions = [
  { title: 'Ledger schema', text: 'Keep the old schema?', place: 2, blocking: false, stakes: 'reversible' as const, suggested: true },
  { title: 'Remove notes', text: 'Delete the notes?', place: 0, blocking: true, stakes: 'irreversible' as const, suggested: true },
  { title: 'Brand copy', text: 'Choose the headline?', place: 0, blocking: true, stakes: 'reversible' as const, suggested: true },
  { title: 'Build parser', text: 'Use the shared lexer?', place: 1, blocking: true, stakes: 'costly' as const, suggested: false },
  { title: 'Ledger import', text: 'Run the migration?', place: 2, blocking: false, stakes: 'reversible' as const, suggested: true },
 ];
 const snapshots = new Map<string, EngineSnapshot>();
 const elsewhere: AttentionItem[] = definitions.map((row, i) => {
  const session = String(i + 2).repeat(16);
  snapshots.set(session, { ...engine.snapshot(), id: session, sessionFile: sessionFileFor(session), title: row.title,
   running: false, entries: [{ Role: 'user', Text: `Context for ${row.title}` }],
   questions: [{ ...question, head: row.text, ask: row.text }], needsPerson: true });
  return { key: `${session}:choice:${question.id}`, session, kind: question.kind, id: question.id,
   title: row.title, text: row.text, answerable: true, sourceFolders: [],
   asked: `2026-10-09T10:0${i}:00Z`, blocking: { turn: row.blocking, tasks: [] }, stakes: row.stakes,
   ...(row.suggested ? { suggestion: { key: 'sqlite', label: 'SQLite', percent: 95, reason: 'One file.' } } : {}),
   placeIds: [places[row.place].id], placeNames: [places[row.place].name] };
 });
 snapshots.set(origin, { ...engine.snapshot(), id: origin, sessionFile: sessionFileFor(origin), title: 'Config stack',
  running: false, entries: [{ Role: 'user', Text: 'Keep this starting context' }], needsPerson: true });
 const here: AttentionItem = { ...elsewhere[2], key: `${origin}:choice:${question.id}`, session: origin,
  text: 'Question in the current conversation', title: 'Config stack', placeIds: [], placeNames: [] };
 await installMockPlaces(page, { places });
 let items = [here, ...elsewhere.filter((_, i) => !options.arrival || i !== 3)];
 const rows: WorldRow[] = [...snapshots].map(([session, snapshot]) => ({ session, title: snapshot.title!,
  sessionFile: snapshot.sessionFile!, project: '', sourceFolders: [], state: 'waiting on you', live: true,
  open: true, running: false, needsYou: true, failed: 0,
  tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 } }));
 let seq = 1;
 const listeners = new Set<() => void>();
 const publish = () => { seq++; for (const wake of listeners) wake(); listeners.clear(); };
 page.once('close', () => { for (const wake of listeners) wake(); listeners.clear(); });
 const answers: { session: string; answer: EngineAnswer }[] = [];
 await page.route('**/api/engine/world', route => route.fulfill({ json: { seq, rows, items } }));
 await page.route('**/api/engine/events?*', async route => {
  const after = Number(new URL(route.request().url()).searchParams.get('after'));
  if (after >= seq) await new Promise<void>(resolve => {
   listeners.add(resolve);
  });
  if (!page.isClosed()) await route.fulfill({ contentType: 'text/event-stream',
   body: `id: ${seq}\ndata: ${JSON.stringify({ seq, type: 'reset', payload: { rows, items } })}\n\n` });
 });
 await page.route('**/api/engine/sessions**', async route => {
  const [, session, action] = new URL(route.request().url()).pathname.split('/').slice(3);
  if (!session) {
   const file = route.request().postDataJSON().sessionFile;
   const snapshot = [...snapshots.values()].find(snapshot => snapshot.sessionFile === file);
   if (snapshot) { await route.fulfill({ json: snapshot }); return; }
  }
  const snapshot = snapshots.get(session);
  if (!snapshot) { await route.fallback(); return; }
  if (!action) { await route.fulfill({ json: snapshot }); return; }
  if (action === 'events') { await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' }); return; }
  if (action === 'answer') {
   const answer = route.request().postDataJSON() as EngineAnswer;
   if (answer.id !== question.id || answer.kind !== question.kind || !question.options.some(option => option.key === answer.key)) {
    await route.fulfill({ status: 409, json: { error: 'Unknown question or option' } }); return;
   }
   answers.push({ session, answer });
   snapshot.questions = []; snapshot.needsPerson = false;
   items = items.filter(item => item.session !== session);
   // The receipt must reach the answering pane before the world drops its question.
   await route.fulfill({ json: { accepted: true } });
   publish(); return;
  }
  await route.fallback();
 });
 await page.addInitScript(({ theme, file }) => {
  Object.defineProperty(navigator, 'platform', { get: () => 'MacIntel' });
  localStorage.setItem('codeaf-theme', theme);
  localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'origin', kind: 'conversation',
   title: 'Config stack', draft: 'Keep my draft', sessionFile: file, pinned: false, titleSource: 'manual' }],
   activeId: 'origin', groups: [], closed: [], nextNumber: 2, recentIds: ['origin'] }));
 }, { theme, file: sessionFileFor(origin) });
 await page.goto('/');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
 return { answers, here, elsewhere, connected: () => listeners.size > 0,
  arrive: () => { items = [...items, elsewhere[3]]; publish(); },
  clearElsewhere: () => { items = [here]; publish(); },
 };
}

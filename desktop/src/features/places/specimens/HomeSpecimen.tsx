import { useMemo, useState } from 'react';
import { Button } from '../../../components/ui';
import { HomePage } from '../HomePage';
import { HomeQuickLook } from '../HomeQuickLook';
import type { HomeChild, HomeConnection, HomeView } from '../home-model';
import type { PlaceActions } from '../place-actions';
import './home-specimen.css';

/** Fixtures for the Design system page and the browser tests only. They are the designer's own names (Places 8a to 8e) and are never shown as a
 * person's places or chats: the real Home gets its `view` from the engine. The clock is fixed so every time label is deterministic. */
export const specimenNow = new Date('2026-10-09T12:00:00Z');
const hoursAgo = (hours: number) => new Date(specimenNow.getTime() - hours * 3_600_000).toISOString();

const place = (id: string, name: string, tint: HomeChild['tint'], places: number, chats: number, extra: Partial<HomeChild> = {}): HomeChild => ({ id, name, tint, places, chats, tintSource: 'own', ...extra });

const codeaf: HomeView = {
  kind: 'place', id: 'pl_codeaf', title: 'codeaf', tint: 'tide', tintSource: 'own', breadcrumb: [], pinned: true,
  recap: { label: 'Since yesterday', text: 'Trailing commas ship outside strict mode, and the v1 port is waiting on your OK. Marketing finished the launch post draft.' },
  attention: [
    { id: 'chat_port', title: 'Port fix to v1 branch', placeName: 'Config parser', status: 'waiting' },
    { id: 'chat_fixtures', title: 'Update fixtures', placeName: 'Config parser', status: 'running', statusText: 'running · 2m' },
  ],
  children: [
    place('pl_software', 'Software', 'tide', 0, 28, { tintSource: 'inherited', status: 'waiting' }),
    place('pl_marketing', 'Marketing', 'rose', 0, 14),
    place('pl_release', 'Release', 'tide', 0, 6, { tintSource: 'inherited', alsoIn: ['Software'], decide: { alwaysAsk: false, threshold: 90 } }),
  ],
  sources: [{ id: 'src_parse', kind: 'folder', label: 'codeaf/internal/parse', state: 'ok' }, { id: 'src_notes', kind: 'file', label: 'release-notes.md', state: 'missing' }],
  chats: [
    { id: 'chat_commas', title: 'Trailing commas across the config stack', excerpt: 'Open · 4 tasks running', status: 'running', at: hoursAgo(1) },
    { id: 'chat_naming', title: 'Naming: codeaf vs CodeAF', excerpt: 'Decided lower-case everywhere', at: hoursAgo(24 * 3 + 2) },
    { id: 'chat_pricing', title: 'Pricing for teams', excerpt: 'Discussed seat vs usage. No decision yet', at: hoursAgo(24 * 9) },
  ],
};

const launch: HomeView = {
  kind: 'place', id: 'pl_launch', title: 'Launch site', tint: 'rose', tintSource: 'inherited', pinned: false,
  breadcrumb: [{ id: 'pl_codeaf', name: 'codeaf' }, { id: 'pl_marketing', name: 'Marketing' }],
  attention: [], children: [], chats: [], contextLine: 'Uses Marketing’s context: brand-voice.md, codeaf.dev',
};

const root: HomeView = {
  kind: 'root', id: 'root', title: 'All places', tint: 'graphite', breadcrumb: [], attention: [],
  totals: { topLevel: 5, all: 58 },
  children: [place('pl_codeaf', 'codeaf', 'tide', 4, 61, { status: 'waiting', decide: { alwaysAsk: false, threshold: 90 } }), place('pl_reports', 'Reports', 'sage', 47, 212), place('pl_personal', 'Personal', 'sand', 0, 8),
    place('pl_reading', 'Reading', 'iris', 3, 19), place('pl_side', 'Side projects', 'rose', 2, 7)],
  allPlaces: [place('pl_codeaf', 'codeaf', 'tide', 4, 61, { status: 'waiting', decide: { alwaysAsk: false, threshold: 90 } }), place('pl_reports', 'Reports', 'sage', 47, 212), place('pl_personal', 'Personal', 'sand', 0, 8),
    place('pl_reading', 'Reading', 'iris', 3, 19), place('pl_side', 'Side projects', 'rose', 2, 7),
    place('pl_papers', 'Papers', 'iris', 0, 11, { tintSource: 'inherited', path: ['Reading'] }), place('pl_q3', 'Q3 report', 'sage', 0, 12, { tintSource: 'inherited', path: ['Reports'] })],
  archivedChildren: [place('pl_old', 'Old experiments', 'sand', 0, 3, { archived: true })],
  unplaced: { total: 38 },
  chats: [
    { id: 'chat_regex', title: 'Quick regex for semver', excerpt: 'Used a two-step match', at: hoursAgo(2) },
    { id: 'chat_generics', title: 'Explain Go generics constraints', excerpt: 'Read only', at: hoursAgo(26) },
  ],
  suggestion: { text: '5 of these look like they belong in Reading', action: 'Move them' },
};

const firstLaunch: HomeView = { ...root, children: [], allPlaces: [], archivedChildren: [], totals: undefined, unplaced: { total: 2 }, suggestion: undefined,
  chats: [{ id: 'chat_repo', title: 'Set up the repo', excerpt: 'Cloned, ran the tests', at: hoursAgo(3) }, { id: 'chat_what', title: 'What is codeaf?', excerpt: 'Read only', at: hoursAgo(4) }] };

const now: HomeView = { kind: 'now', id: 'now', title: 'Now', tint: 'graphite', breadcrumb: [], attention: [], children: [],
  chats: [{ id: 'chat_regex', title: 'Quick regex for semver', excerpt: 'Used a two-step match', at: hoursAgo(2) }] };

const reading: HomeView = { kind: 'place', id: 'pl_reading', title: 'Reading', tint: 'iris', tintSource: 'own', breadcrumb: [], attention: [],
  recap: { label: 'Since last week', text: 'You finished the Raft notes and started on Spanner. Nothing is running.' },
  children: [place('pl_papers', 'Papers', 'iris', 0, 11, { tintSource: 'inherited' }), place('pl_books', 'Books', 'iris', 0, 5, { tintSource: 'inherited' }), place('pl_talks', 'Talks', 'iris', 0, 3, { tintSource: 'inherited' })],
  chats: [{ id: 'chat_raft', title: 'Raft, chapter by chapter', excerpt: 'Open questions on log compaction', at: hoursAgo(24 * 5) }, { id: 'chat_spanner', title: 'Spanner TrueTime', excerpt: 'Started', at: hoursAgo(24 * 6) }] };

export const homeScenarios = { place: codeaf, empty: launch, root, first: firstLaunch, now, loading: undefined, error: undefined, offline: codeaf } as const;
export type HomeScenario = keyof typeof homeScenarios;
const connections: Partial<Record<HomeScenario, HomeConnection>> = {
  loading: { state: 'loading' }, error: { state: 'error', message: 'Could not read your places. Nothing was changed.' }, offline: { state: 'offline' },
};

/** One Home in one scenario with every verb wired to a local log, so the browser tests can watch each callback and the page's own reactions. */
export function HomeSpecimen({ scenario = 'place', withoutVerbs = false }: { scenario?: HomeScenario; withoutVerbs?: boolean }) {
  const [log, setLog] = useState<string[]>([]);
  const [lookAt, setLookAt] = useState<string>();
  const [view, setView] = useState(homeScenarios[scenario]);
  const say = (line: string) => setLog(previous => [...previous.slice(-11), line]);
  const patch = (update: (children: readonly HomeChild[]) => HomeChild[]) => setView(current => current && { ...current, children: update(current.children) });

  const actions = useMemo<PlaceActions>(() => withoutVerbs ? {} : {
    goTo: id => say(`goTo:${id}`),
    goToInNewWindow: id => say(`newWindow:${id}`),
    quickLook: id => { say(`quickLook:${id}`); setLookAt(id); },
    openChat: id => say(`openChat:${id}`),
    openChatInNewTab: id => say(`openChatNewTab:${id}`),
    create: async draft => {
      if (draft.name === 'Fails') throw new Error('That would put “Fails” inside “Fails”.');
      say(`create:${draft.name}:${draft.tint ?? 'none'}:${draft.parent ?? 'root'}`);
      patch(children => [...children, place(`pl_${draft.name.toLowerCase().replace(/\W+/g, '')}`, draft.name, draft.tint ?? 'tide', 0, 0, { tintSource: draft.tint ? 'own' : 'inherited' })]);
    },
    rename: (id, name) => { say(`rename:${id}:${name}`); patch(children => children.map(child => child.id === id ? { ...child, name } : child)); },
    setTint: (id, tint) => { say(`tint:${id}:${tint}`); patch(children => children.map(child => child.id === id ? { ...child, tint, tintSource: 'own' } : child)); },
    setDecide: (id, change) => {
      say(`decide:${id}:${change.alwaysAsk ?? ''}:${change.threshold ?? ''}`);
      patch(children => children.map(child => child.id === id && child.decide ? { ...child, decide: { ...child.decide, ...change } } : child));
    },
    pin: id => say(`pin:${id}`),
    unpin: id => say(`unpin:${id}`),
    archive: id => { say(`archive:${id}`); patch(children => children.filter(child => child.id !== id)); },
    restore: id => say(`restore:${id}`),
    loadDeletePreview: async id => { say(`preview:${id}`); return { children: 1, chatsHere: 6, wouldBeUnplaced: ['chat_x', 'chat_y'] }; },
    remove: id => { say(`remove:${id}`); patch(children => children.filter(child => child.id !== id)); },
    chooseMergeTarget: id => say(`chooseMerge:${id}`),
    chooseAnotherParent: id => say(`chooseParent:${id}`),
    file: (drop, target, mode) => say(`file:${drop.kind}:${drop.ids.join(',')}:${target}:${mode}`),
    removeChat: (chat, from) => say(`removeChat:${chat}:${from}`),
    chooseChatPlace: id => say(`chooseChatPlace:${id}`),
    addSources: id => say(`addSources:${id}`),
    removeSource: (id, source) => say(`removeSource:${id}:${source}`),
    writeInstructions: id => say(`writeInstructions:${id}`),
    acceptSuggestion: () => say('acceptSuggestion'),
    openFolderAsPlace: () => say('openFolder'),
    retry: () => say('retry'),
  }, [withoutVerbs]);
  const looked = lookAt ? (lookAt === reading.id ? reading : view?.children.find(child => child.id === lookAt) && { ...reading, id: lookAt, title: view?.children.find(child => child.id === lookAt)?.name ?? reading.title }) : undefined;

  return <div className="home-specimen" data-testid="home-specimen" data-scenario={scenario}>
    <div className="home-specimen-frame">
      <HomePage view={view} connection={connections[scenario]} actions={actions} now={specimenNow} newWindowHint="⌘↵"
        composer={<div className="home-specimen-composer">{view ? `Start something in ${view.title}` : 'Start something'}</div>}/>
      {looked && <HomeQuickLook view={looked} actions={actions} now={specimenNow} onClose={() => { say('quickLook:closed'); setLookAt(undefined); }}/>}
    </div>
    <ol className="home-specimen-log" aria-label="Callback log">{log.map((line, index) => <li key={index}>{line}</li>)}</ol>
    <Button variant="quiet" onClick={() => setLog([])}>Clear log</Button>
  </div>;
}

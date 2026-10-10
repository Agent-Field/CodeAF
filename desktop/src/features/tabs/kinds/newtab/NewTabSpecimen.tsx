import { SectionHeading, Surface, Text } from '../../../../components/ui';
import type { Tab } from '../../types';
import { NewTabView } from './NewTabView';
import type { HistoryItem } from '../../../history/types';
import { matchesOf } from './historyRows';
import { buildSections } from './rows';

// Specimen data: drawn only on the Design system page, never in a workspace (engine-truth law).
const tab = (id: string, title: string): Tab => ({ id, kind: 'conversation', title, draft: '', pinned: false });
const conversation = (id: string, title: string, archived = false): HistoryItem => ({ id, sessionFile: `/specimen/${id}`, title, at: '2026-10-06T14:02:00Z', messages: 9, tasks: 0, tasksRunning: 0, files: [], fileCount: 0, decisions: 0, state: 'idle', open: false, archived });
const history = matchesOf('fix', [{ id: 'a', line: 'decided: fix in the lexer, keep strict mode', item: conversation('a', 'Fix it in the lexer') }, { id: 'b', line: '', item: conversation('b', 'Error positions point at the wrong file', true) }], new Map(), 14);
// A fixed clock so the specimen's "closed 1 hour ago" does not drift as the page sits open.
const specimenNow = Date.UTC(2026, 9, 10, 16, 0, 0);
const typed = buildSections({
  query: 'fix',
  tabs: [{ tab: tab('t', 'Port fix to v1 branch'), shortcut: '⌘3', waiting: true }],
  closed: [{ ...tab('c', 'Fix it in the lexer'), closedAt: specimenNow - 3_600_000 }],
  now: specimenNow,
  files: [{ path: 'internal/parse/testdata/fixtures.go', name: 'fixtures.go', dir: 'internal/parse/testdata' }],
  terminal: true, terminalShortcut: '⌃`', fileShortcut: '⌘O', history, seeAllShortcut: '⌘↵',
});
const empty = buildSections({ query: '', tabs: [], closed: [], files: [], terminal: true, terminalShortcut: '⌃`', fileShortcut: '⌘O' });
const caption = 'Type a question, a file, a URL, or a command.';

/** The new-tab field, typed and empty (design 3f, Components "Command field"). Specimen only: none of this is live data. */
export function NewTabSpecimen() {
  return <Surface direction="column">
    <SectionHeading>New tab field</SectionHeading>
    <Text>Specimen. One field: the first row turns what you typed into a conversation; below it come starting points, matching files, open tabs and recently closed tabs.</Text>
    <div className="newtab-specimen" data-newtab-specimen>
      <NewTabView id="specimen-typed" field={<span className="newtab-typed">fix<span className="newtab-caret"/></span>} query="fix" sections={typed} activeRowId="ask" caption={caption}/>
      <NewTabView id="specimen-empty" field={<span className="newtab-typed newtab-placeholder"/>} query="" sections={empty} activeRowId="terminal" caption={caption}/>
    </div>
  </Surface>;
}

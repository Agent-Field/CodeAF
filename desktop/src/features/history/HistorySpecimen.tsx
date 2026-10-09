import { SectionHeading, Surface, Text } from '../../components/ui';
import { ArchiveToast } from './ArchiveToast';
import { HistoryRow } from './HistoryRow';
import { BestMatchCard } from './SearchResults';
import type { HistoryItem } from './types';
import './history.css';

// Specimen only: these rows are drawn from the design's own words (Shell 4a, 4b, 4c), never from a live engine.
const now = new Date(2026, 9, 9, 15, 0).getTime();
const at = (day: number, hour: number, minute = 0) => new Date(2026, 9, day, hour, minute).toISOString();
const base: Omit<HistoryItem, 'id' | 'title' | 'at'> = { sessionFile: '', messages: 0, tasks: 0, tasksRunning: 0, files: [], fileCount: 0, decisions: 0, state: 'idle', open: false, archived: false };
const rows: { item: HistoryItem; selected?: boolean }[] = [
  { item: { ...base, id: 'specimen-working', title: 'Trailing commas across the config stack', at: at(9, 10, 40), open: true, state: 'working', tasks: 5, tasksRunning: 4 } },
  { item: { ...base, id: 'specimen-waiting', title: 'Release v2.4', at: at(9, 10, 15), open: true, state: 'needs-you', reason: 'Tag v2.4.1?' } },
  { item: { ...base, id: 'specimen-files', title: 'Fix it in the lexer', at: at(6, 14, 2), line: 'Decided to keep strict mode as the default and fix it in the lexer', files: [{ path: 'internal/config/lexer.go', added: 12, removed: 3 }, { path: 'testdata/nested.json', added: 6, removed: 0 }], fileCount: 2 }, selected: true },
  { item: { ...base, id: 'specimen-plain', title: 'Naming for the config loader API', at: at(1, 16, 20), line: 'Discussed Load vs Open. No decision yet' } },
];
const noop = () => {};

/** The History row in each state, the best-match card and the auto-archive toast (Components "Shell · history"). */
export function HistorySpecimen() {
  return <Surface direction="column">
    <SectionHeading>History</SectionHeading>
    <Text>Specimen. Rows lead with substance: the title, one sentence of what was discussed or decided, the changed files, and the time. Live conversations show their state.</Text>
    <div className="history-specimen-stack" role="listbox" aria-label="History row specimen">
      {rows.map(({ item, selected }) => <HistoryRow key={item.id} item={item} now={now} selected={!!selected} domId={item.id} onSelect={noop} onOpen={noop}/>)}
    </div>
    <span className="history-specimen-label">Best match</span>
    <div className="history-specimen-best"><BestMatchCard best={{ item: rows[2].item, answer: 'Decided to keep strict mode as the default and fix it in the lexer', terms: ['lexer'] }} now={now} terms={['lexer']} onRecap={noop} onJump={noop} onContinue={noop}/></div>
    <span className="history-specimen-label">Toast · auto-archive</span>
    <div className="history-specimen-toast"><ArchiveToast count={6} onReview={noop} onRestore={noop} onDismiss={noop}/></div>
  </Surface>;
}

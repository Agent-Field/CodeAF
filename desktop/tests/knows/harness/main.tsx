import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import '../../../src/design/inputModality';
import '../../../src/App.css';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { ToastRegion } from '../../../src/components/ui/Toast';
import { KnowsList, type KnowsChange } from '../../../src/features/places/knows/KnowsList';
import type { KnowsLine } from '../../../src/features/places/knows/model';

const now = new Date('2026-10-10T12:00:00Z');
const initial: KnowsLine[] = [
  { id: 'a', placeId: 'p', text: 'Run the v1 suite before any launch goes out', source: { kind: 'said-in-chat', chatId: 'chat', at: '2026-10-06T12:00:00Z' }, createdAt: '2026-10-09T12:00:00Z' },
  { id: 'b', placeId: 'p', text: 'Lead pricing with free for one seat', source: { kind: 'learned', answers: 3 }, createdAt: '2026-10-08T12:00:00Z' },
  { id: 'c', placeId: 'p', text: 'Lead with usage pricing', source: { kind: 'you-wrote' }, createdAt: '2026-10-07T12:00:00Z', replacedBy: 'b', replacedAt: '2026-10-06T12:00:00Z' },
  { id: 'd', placeId: 'p', text: 'Use plain words', source: { kind: 'you-wrote' }, createdAt: '2026-07-01T12:00:00Z' },
];
function Harness() {
  const params = new URLSearchParams(location.search);
  const [lines, setLines] = useState(params.has('empty') ? [] : initial);
  const [confirmed, setConfirmed] = useState('');
  const change = (next: KnowsLine[]): KnowsChange => {
    const previous = lines;
    setLines(next);
    return { undo: () => setLines(previous) };
  };
  return <><KnowsList placeName="Marketing" lines={lines} now={now} chatTitles={{ chat: 'Launch post' }} readOnly={params.has('readonly')} actions={{
    add: async text => {
      if (text === 'refuse') throw new Error('The engine refused this line.');
      return change([...lines, { id: `added-${lines.length}`, placeId: 'p', text, source: { kind: 'you-wrote' }, createdAt: now.toISOString() }]);
    },
    edit: async (id, text) => change(lines.map(line => line.id === id ? { ...line, text } : line)),
    remove: async id => change(lines.filter(line => line.id !== id)),
    confirm: async id => { setConfirmed(id); setLines(lines.map(line => line.id === id ? { ...line, askedStillTrueAt: now.toISOString() } : line)); },
  }}/><output aria-label="Confirmed line">{confirmed}</output><ToastRegion/></>;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness/></ThemeProvider>);

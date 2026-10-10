// Test-only entry for the ⌘P palette: it mounts the palette alone over a fixture graph (the designer's names from Places 6c,
// plus 200 generated places for the windowing check) and logs every callback, so no test depends on the app shell.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import React, { useState } from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { GoToPalette } from '../../../src/features/places/palette/GoToPalette';
import type { PlaceRowModel } from '../../../src/features/places/shell/contracts';

const now = new Date('2026-10-09T12:00:00Z');
const ago = (minutes: number) => new Date(now.getTime() - minutes * 60_000).toISOString();
const row = (id: string, name: string, tint: PlaceRowModel['tint'], extra: Partial<PlaceRowModel> = {}): PlaceRowModel => ({ id, name, tint, pinned: false, archived: false, ...extra });

const big = new URLSearchParams(location.search).has('big');
const places: PlaceRowModel[] = [
  row('codeaf', 'codeaf', 'tide', { meta: '2 inside', lastOpenedAt: ago(300) }),
  row('software', 'Software', 'tide', { meta: '3 chats', lastOpenedAt: ago(10) }),
  row('config', 'Config parser', 'tide', { meta: '9 chats', status: 'waiting', statusLabel: '2 need you', lastOpenedAt: ago(2) }),
  row('reports', 'Reports', 'sage', { meta: big ? '200 inside' : '1 inside' }),
  row('q3', 'Q3 report', 'sage', { meta: '12 chats', lastOpenedAt: ago(60 * 24 * 3) }),
];
const children = new Map<string, string[]>([['root', ['codeaf', 'reports']], ['codeaf', ['software', 'config']], ['reports', ['q3']]]);
if (big) for (let at = 0; at < 200; at++) { places.push(row(`g${at}`, `Generated ${at}`, 'iris')); children.get('reports')!.push(`g${at}`); }

function Harness() {
  const [open, setOpen] = useState(false);
  const [log, setLog] = useState<string[]>([]);
  const note = (line: string) => setLog(previous => [...previous, line]);
  return <main>
    <button id="opener" onClick={() => setOpen(true)}>Open</button>
    <ol aria-label="Callback log">{log.map((line, at) => <li key={at}>{line}</li>)}</ol>
    <GoToPalette open={open} places={places} childrenOf={children} now={now}
      onOpen={id => { note(`open ${id}`); setOpen(false); }} onOpenInNewWindow={id => note(`window ${id}`)}
      onCreate={name => note(`create ${name}`)} onClose={() => setOpen(false)}/>
  </main>;
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(<React.StrictMode><ThemeProvider><Harness/></ThemeProvider></React.StrictMode>);

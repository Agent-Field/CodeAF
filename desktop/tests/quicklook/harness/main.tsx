import '../../../src/design/inputModality';
import '../../../src/App.css';
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { HomePage } from '../../../src/features/places/HomePage';
import { QuickLook } from '../../../src/features/places/QuickLook';
import type { HomeView } from '../../../src/features/places/home-model';

const now = new Date('2026-10-10T12:00:00Z');
const noActions = new URLSearchParams(location.search).has('bare');
const empty = new URLSearchParams(location.search).has('empty');
const place: HomeView = {
  kind: 'place', id: 'reading', title: 'Reading', tint: 'iris', breadcrumb: [], attention: [],
  recap: empty ? undefined : { label: 'Since last week', text: 'You finished the Raft notes.' },
  children: empty ? [] : [{ id: 'papers', name: 'Papers', tint: 'iris', chats: 2, places: 0 }],
  chats: empty ? [] : Array.from({ length: 4 }, (_, i) => ({ id: `chat-${i}`, title: `Chat ${i + 1}`, at: '2026-10-10T11:00:00Z' })),
};
const root: HomeView = { kind: 'root', id: 'root', title: 'All places', tint: 'graphite', breadcrumb: [], attention: [], chats: [],
  children: [{ id: place.id, name: place.title, tint: place.tint, chats: 4, places: 1 }] };

function Harness() {
  const [open, setOpen] = useState(false);
  const [calls, setCalls] = useState<string[]>([]);
  const record = (action: string) => setCalls(previous => [...previous, action]);
  const actions = { quickLook: () => setOpen(true), goTo: (id: string) => record(`go:${id}`), goToInNewWindow: (id: string) => record(`window:${id}`), openChat: () => record('chat') };
  return <div data-testid="window" data-tint="graphite"><HomePage view={root} actions={actions} now={now}/>
    <output aria-label="Actions">{calls.join(',')}</output>
    {open && <QuickLook view={place} actions={noActions ? {} : actions} now={now} chatLimit={10} onClose={() => setOpen(false)}/>}
  </div>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><ThemeProvider><Harness/></ThemeProvider></React.StrictMode>);

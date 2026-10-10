import { createElement as h, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { PlaceRail } from '../../../src/features/shell/PlaceRail';
import type { PlaceRowModel } from '../../../src/features/places/shell/contracts';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** Real rail wiring with labelled fixture data proves that close never becomes the row's navigation action. */
export function mountRailRows(theme: string) {
  localStorage.setItem('codeaf-theme', theme);
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  const place = (id: string, name: string, extra = {}): PlaceRowModel => ({ id, name, tint: 'rose', pinned: false, archived: false, ...extra });
  function Demo() {
    const [open, setOpen] = useState([
      place('marketing', 'Marketing', { parentName: 'codeaf', status: 'waiting', statusLabel: '2 need you in Marketing' }),
      place('failed', 'Config parser', { status: 'failed' }),
      place('closed', 'Background', { closedButBusy: true, status: 'waiting' }),
      place('long', 'A place with a very long title that cannot fit in the rail', { parentName: 'A long parent' }),
    ]);
    const [visited, setVisited] = useState('');
    return h('div', { className: 'rail-row-fixture' }, h(PlaceRail, {
      inert: false, peeking: false, onToggle: () => {}, current: 'marketing',
      now: { active: false, count: 3, shortcut: '⌃0', onGo: () => setVisited('now') },
      sections: { pinned: [place('pinned', 'Personal', { pinned: true, tint: 'sand' })], open },
      actions: { go: setVisited, close: id => setOpen(rows => rows.filter(row => row.id !== id)), closeAll: () => setOpen([]), closeOthers: () => {} },
      tabCount: () => 4, slotShortcut: index => `⌃${index}`, closeShortcut: '⌘⇧W', newWindowShortcut: '⌘N', appItems: [],
    }), h('output', { id: 'visited' }, visited));
  }
  createRoot(host).render(h(ThemeProvider, null, h(Demo)));
}

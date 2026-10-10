import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { Button, DropdownMenu } from '../../../src/components/ui';
import { PlaceRail, type PlaceRailProps } from '../../../src/features/shell/PlaceRail';
import { placeSwitcher } from '../../../src/features/places/shell/placeSwitcher';
import { switcherRowContents } from '../../../src/features/places/rail/switcherRowContents';
import type { PlacesShell } from '../../../src/features/places/shell/PlacesShell';
import type { PlaceRowModel } from '../../../src/features/places/shell/contracts';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** The real rail and shared menu are mounted against explicit specimen data; every gesture records its actual callback. */
export function mountPlaceRail(theme: string, empty = false) {
  localStorage.setItem('codeaf-theme', theme);
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  const row = (id: string, name: string, extra = {}): PlaceRowModel => ({ id, name, tint: 'tide', pinned: false, archived: false, ...extra });
  function Specimen() {
    const [pinned, setPinned] = useState(empty ? [] : [row('codeaf', 'codeaf', { pinned: true }), row('personal', 'Personal', { pinned: true, tint: 'sand' })]);
    const [open, setOpen] = useState([row('config', 'Config parser', { parentName: 'codeaf', status: 'waiting', statusLabel: '2 need you in Config parser' }), row('marketing', 'Marketing', { tint: 'rose', status: 'failed', statusLabel: '1 failed task in Marketing' })]);
    const [calls, setCalls] = useState<string[]>([]);
    const [current, setCurrent] = useState('codeaf');
    const call = (text: string) => setCalls(calls => [...calls, text]);
    const props: PlaceRailProps = {
      inert: false, peeking: false, onToggle: () => {}, current,
      now: { active: current === 'now', shortcut: '⌃0', onGo: () => { call('go:now'); setCurrent('now'); } },
      sections: { pinned, open }, allPlaces: { active: false, shortcut: '⌘⇧P', onOpen: () => call('all'), onOpenInNewTab: () => call('all:tab') },
      actions: {
        go: id => { call(`go:${id}`); setCurrent(id); }, close: id => { call(`close:${id}`); setOpen(open => open.filter(row => row.id !== id)); },
        closeAll: () => { call('close:all'); setOpen([]); }, closeOthers: id => setOpen(open => open.filter(row => row.id === id)),
        pin: (id, index) => { call(`pin:${id}:${index ?? 'end'}`); const place = [...pinned, ...open].find(row => row.id === id)!; setPinned(rows => { const next = rows.filter(row => row.id !== id); next.splice(index ?? next.length, 0, { ...place, pinned: true }); return next; }); setOpen(rows => rows.filter(row => row.id !== id)); },
        unpin: id => { call(`unpin:${id}`); const place = pinned.find(row => row.id === id)!; setPinned(rows => rows.filter(row => row.id !== id)); setOpen(rows => [{ ...place, pinned: false }, ...rows]); },
        quickLook: id => call(`look:${id}`), newWindow: id => call(`window:${id}`), rename: id => call(`rename:${id}`),
        setTint: (id, tint) => call(`tint:${id}:${tint}`), fileChats: (ids, id) => call(`file:${ids.join(',')}:${id}`),
      },
      slotShortcut: index => `⌃${index}`, closeShortcut: '⌘⇧W', newWindowShortcut: '⌘↵',
    };
    const switcher = placeSwitcher({ place: current } as PlacesShell, props, switcherRowContents);
    return <><div className="rail-row-fixture"><PlaceRail {...props}/></div><DropdownMenu label="Place switcher" className="place-switcher" items={switcher.items}><Button>Switch place</Button></DropdownMenu><output id="rail-calls">{calls.join('|')}</output></>;
  }
  createRoot(host).render(<ThemeProvider><Specimen/></ThemeProvider>);
}

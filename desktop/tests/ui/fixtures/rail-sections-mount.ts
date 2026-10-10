import { createElement as h, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { PlaceRail } from '../../../src/features/shell/PlaceRail';
import type { PlaceRowModel } from '../../../src/features/places/shell/contracts';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';
import '../../../src/App.css';

export type RailScene = 'full' | 'empty' | 'zero' | 'archived';

const place = (id: string, name: string, extra: Partial<PlaceRowModel> = {}): PlaceRowModel => ({ id, name, tint: 'tide', pinned: false, archived: false, ...extra });

/** A labelled rail for the section law: order, empty copy, zero places, archived rows, and the narrow drawer. */
export function mountRailSections(theme: string, scene: RailScene, drawer = false) {
  localStorage.setItem('codeaf-theme', theme);
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  const pinned = scene === 'full' ? [place('codeaf', 'codeaf', { pinned: true, tint: 'tide' })] : [];
  const open = scene === 'full' ? [place('config', 'Config parser', { parentName: 'codeaf', tint: 'rose' })] : [];
  if (scene === 'archived') {
    pinned.push(place('old-pin', 'Old pin', { pinned: true, archived: true }));
    open.push(place('old-open', 'Old open', { archived: true }));
  }
  const livePlaces = scene === 'zero' ? 0 : scene === 'empty' ? 4 : scene === 'archived' ? 1 : 2;
  function Demo() {
    const [log, setLog] = useState('');
    const note = (line: string) => setLog(line);
    const rail = h(PlaceRail, {
      inert: false, peeking: false, onToggle: () => note('toggle'),
      now: { active: scene !== 'full', count: scene === 'zero' ? 0 : 2, shortcut: '⌃0', onGo: () => note('now'), onNewWindow: () => note('now-window') },
      sections: { pinned, open },
      current: scene === 'full' ? 'config' : undefined,
      emptyHint: scene === 'empty',
      livePlaces,
      allPlaces: { active: false, shortcut: '⌘⇧P', onOpen: () => note('all'), onOpenInNewTab: () => note('all-tab') },
      actions: {
        go: id => note(`go:${id}`), newWindow: id => note(`window:${id}`), close: id => note(`close:${id}`),
        closeAll: () => note('close-all'), closeOthers: () => note('close-others'),
      },
      slotShortcut: index => `⌃${index}`, closeShortcut: '⌘⇧W', newWindowShortcut: '⌘N',
    });
    const body = drawer
      ? h('div', { className: 'app-shell sidebar-collapsed' }, h('dialog', { className: 'sidebar-drawer', open: true, 'aria-label': 'Navigation' }, rail))
      : h('div', { className: 'app-shell' }, rail);
    return h('div', null, body, h('output', { id: 'rail-log' }, log));
  }
  createRoot(host).render(h(ThemeProvider, null, h(Demo)));
}

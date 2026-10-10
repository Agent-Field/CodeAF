import { FoundationsSpecimen } from './components/specimens/FoundationsSpecimen';
import { PrimitivesSpecimen } from './components/specimens/PrimitivesSpecimen';
import { WebAddressSpecimen } from './features/web/WebAddressSpecimen';
import { TerminalSpecimen } from './features/terminal/TerminalSpecimen';
import { NewTabSpecimen } from './features/tabs/kinds/newtab/NewTabSpecimen';
import { FilesSpecimen } from './features/files/specimens/FilesSpecimen';
import { PreviewSpecimen } from './features/tabs/specimens/PreviewSpecimen';
import { useCallback, useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { connectDesktopTabs } from './lib/desktopTabs';
import { Button, IconButton, Icon, PageHeading, SectionHeading, Text, CodeText, Markdown, Surface, ContextMenu, DropdownMenu, WorkStateIndicator, ToastHost, iconNames, type MenuEntry } from './components/ui';
import design from './design/tokens.json';
import { useMediaQuery } from './design/useMediaQuery';
import './styles/frame-material.css';
import './features/shell/touch.css';
import './App.css';
import { useWindowActive } from './design/useWindowActive';
import { windowPlace } from './lib/native/windowPlace';
import { useWindowKeys } from './features/shell/useWindowKeys';
import { focusHistoryStorageKey, Workspace } from './features/tabs/Workspace';
import { restoreNamedScroll } from './features/tabs/scroll/memoryStore';
import type { FocusEntry } from './features/focus-history/model';
import { FocusHistoryProvider, useFocusWireFor } from './features/focus-history/useFocusHistory';
import { createNextUp, NextUpProvider } from './features/nextup/useNextUp';
import { PlaceRail } from './features/shell/PlaceRail';
import { PlacesShellProvider, usePlacesShellController, type PlacesShell } from './features/places/shell/PlacesShell';
import { PlacesOverlays } from './features/places/shell/PlacesOverlays';
import { switcherRowContents } from './features/places/rail/switcherRowContents';
import { placeSwitcher, usePlaceKeys, usePlaceRail, useWindowTint } from './features/places/shell/useShellPlaces';
import { requestWorkspace } from './features/places/shell/workspaceBus';
import { GoToChooserSpecimen } from './features/places/shell/GoToChooserSpecimen';
import { PlaceDialogsSpecimen, ToastSpecimen } from './features/places/shell/PlaceDialogsSpecimen';
import { chatIdFromSessionFile } from './features/places/client';
import { homeMenu } from './features/places/place-actions';
import { writeDecide } from './features/places/PlaceMenuDecide';
import { placeShortcuts } from './design/keyboard';
import { TabsSpecimen } from './features/tabs/specimens/TabsSpecimen';
import { GroupOfferSpecimen } from './features/tabs/specimens/GroupOfferSpecimen';
import { RailSpecimen } from './features/shell/specimens/RailSpecimen';
import { RailRowsSpecimen } from './features/shell/specimens/RailRowsSpecimen';
import { PlacesSpecimen } from './features/places/specimens/PlacesSpecimen';
import { HistorySpecimen } from './features/history/HistorySpecimen';
import { ControlsSpecimen } from './components/specimens/ControlsSpecimen';
import { ConversationSpecimens } from './features/conversation/specimens/ConversationSpecimens';
import { RailToggle } from './features/shell/RailToggle';
import { useShellFrame } from './features/shell/useShellFrame';
import { devPageEvent, requestLeaveKind, requestOpenKind, useActiveHome, useActiveKind } from './features/shell/shellState';
import { shortcutLayer } from './design/keyboard';
import { useShortcuts } from './design/useShortcuts';
import { requestNewTabField } from './features/shell/newTabField';
import { usePaletteKey } from './features/shell/usePaletteKey';

/** Settings is a tab of the workspace (its own kind), so it is not a page here: its rail item opens that tab. */
type Page = 'Workspace' | 'Design system';
const devPages: readonly Page[] = ['Workspace', 'Design system'];
const desktop = isTauri();
const mac = desktop && /Mac/.test(navigator.platform);
document.documentElement.dataset.environment = mac ? 'mac-desktop' : desktop ? 'desktop' : 'browser';

/**
 * Puts focus back on a history step. The same place selects at once. Another place waits until that
 * strip is listening: Go to resolves after the engine, and the select is one turn later so it is not dropped.
 * A drill is the task id the workspace recorded; none means the tab's own page.
 */
function restoreFocus(shell: PlacesShell, entry: FocusEntry) {
 const task = entry.drillPath[0];
 const apply = () => {
  // Before the pane mounts: a place switch has already dropped this pane from scroll memory.
  restoreNamedScroll(entry.tabId, entry.scroll);
  requestWorkspace({ type: 'select', id: entry.tabId });
  requestWorkspace({ type: 'view', id: entry.tabId, change: { route: task ? { taskId: task, back: [], forward: [] } : undefined } });
 };
 if (entry.windowPlace === shell.place) { apply(); return; }
 void shell.goTo(entry.windowPlace).then(() => { window.setTimeout(apply, 0); }).catch(shell.warn);
}

function App() {
 useEffect(connectDesktopTabs, []);
 useWindowActive();
 useWindowKeys();
 // Boot is synchronous so the first workspace reads this window's destination and storage key.
 const [boot] = useState(windowPlace);
 const [page, setPage] = useState<Page>('Workspace');
 const narrow = useMediaQuery(`(max-width: ${design.breakpoints.small}px)`);
 const frame = useShellFrame(narrow);
 const activeKind = useActiveKind();
 const [drawerOpen, setDrawerOpen] = useState(false);
 const drawer = useRef<HTMLDialogElement>(null);
 const sidebarToggle = useRef<HTMLButtonElement>(null);
 const sidebarHidden = frame.railHidden;
 useEffect(() => {
  if (narrow && drawerOpen) { if (!drawer.current?.open) drawer.current?.showModal(); }
  else { drawer.current?.close(); setDrawerOpen(false); }
 }, [narrow, drawerOpen]);
 function navigate(next: Page) { setPage(next); setDrawerOpen(false); }
 function toggleSidebar() { if (narrow) setDrawerOpen(open => !open); else frame.toggleRail(); }
 // The app layer of the shell's one shortcut registry (design/keyboard.ts): the rail and Focus mode work on every page (⌘K: usePaletteKey).
 useShortcuts(shortcutLayer.app, shortcut => {
  if (shortcut.id === 'rail') { toggleSidebar(); return true; }
  if (shortcut.id === 'focus') { frame.toggleFocus(); return true; }
  if (shortcut.id === 'settings') { setDrawerOpen(false); setPage('Workspace'); requestOpenKind('settings'); return true; }
  return false;
 });
 // The development pages have no chrome of their own now that the palette is retired; tests and developers reach them by this event.
 useEffect(() => {
  if (!import.meta.env.DEV) return;
  const onPage = (event: Event) => { const next = (event as CustomEvent<Page>).detail; if (devPages.includes(next)) navigate(next); };
  window.addEventListener(devPageEvent, onPage);
  return () => window.removeEventListener(devPageEvent, onPage);
 }, []);
 const settingsOpen = page === 'Workspace' && activeKind === 'settings';
 const shell = usePlacesShellController(boot);
 const activeHome = useActiveHome();
 // Every place verb lands in the workspace: going somewhere from another page brings the workspace back.
 const enterWorkspace = useCallback(() => { setDrawerOpen(false); setPage('Workspace'); if (activeKind === 'settings') requestLeaveKind('settings'); }, [activeKind]);
 usePlaceKeys(shell, enterWorkspace);
 usePaletteKey(enterWorkspace);
 // The shell wears this name so the frame tokens resolve on the window. The hook also writes it on document.body before paint, so a menu portalled onto the body inherits the same tokens. Now and the root are graphite; a place the graph has not named wears nothing.
 const frameTint = useWindowTint(shell);
 // One controller per window. Skip and a pending Accept stay here, not on the module singleton.
 const [nextUpWindow] = useState(() => createNextUp());
 const shellNow = useRef(shell);
 shellNow.current = shell;
 const focusWire = useFocusWireFor(focusHistoryStorageKey(boot.label), entry => restoreFocus(shellNow.current, entry));
 // No Inbox row. Questions in other conversations are the strip's frame pill, fed by nextUpWindow.
 // Settings is the bottom row. Design system sits above it only in a development build. Theme stays in that tab.
 const rail = usePlaceRail(shell, {
  inert: narrow ? !drawerOpen : sidebarHidden && frame.peek !== 'rail', peeking: frame.peek === 'rail',
  onToggle: () => narrow ? setDrawerOpen(false) : toggleSidebar(),
  settings: { active: settingsOpen, onOpen: () => { navigate('Workspace'); requestOpenKind('settings'); } },
  designSystem: import.meta.env.DEV ? { active: page === 'Design system', onOpen: () => navigate('Design system') } : undefined,
  onWorkspace: page === 'Workspace' && !settingsOpen, activeHome, onEnterWorkspace: enterWorkspace,
 });
 const sidebar = <PlaceRail {...rail}/>;
 const current = shell.place === 'now' ? undefined : shell.index?.byId.get(shell.place);
 // A chat started in a place's strip is filed there between its creation and its first turn (conversation/firstTurn.ts).
 const firstTurn = useCallback(async (sessionFile: string) => {
  if (shell.place === 'now') return;
  const chatId = chatIdFromSessionFile(sessionFile);
  if (!chatId) throw new Error('The engine did not say where the new chat is saved, so it was not filed. Nothing was sent.');
  try { await shell.client.addChats(shell.place, [chatId], { addedBy: 'you' }); }
  catch (failure) { throw new Error(`This chat could not be filed in “${current?.name ?? 'this place'}”: ${failure instanceof Error ? failure.message : 'the engine refused'}. Nothing was sent; your words are kept.`); }
  void shell.refresh();
 }, [shell.place, shell.client, current?.name]);
 const placeMenu: MenuEntry[] | undefined = current ? [...homeMenu({ id: current.id, name: current.name, tint: current.effectiveTint, pinned: current.pinned, decide: current.decide }, {
  setDecide: (id, change) => void writeDecide(shell, id, current.name, current.decide, change).catch(shell.warn),
  goToInNewWindow: id => shell.goToInNewWindow(id),
  pin: id => void shell.write(`Pinned “${current.name}” to the rail`, () => shell.client.pinPlace(id), { subject: current.name }).catch(shell.warn),
  unpin: id => void shell.write(`Unpinned “${current.name}”`, () => shell.client.unpinPlace(id), { subject: current.name }).catch(shell.warn),
  setTint: (id, next) => void shell.write(`Changed the tint of “${current.name}”`, () => shell.client.updatePlace(id, { tint: next }), { subject: current.name }).catch(shell.warn),
  chooseAnotherParent: id => shell.openChooser({ kind: 'parent', placeId: id, placeName: current.name }),
  chooseMergeTarget: id => shell.openChooser({ kind: 'merge', placeId: id, placeName: current.name }),
 }, { canRename: true, startRename: id => shell.openDialog({ kind: 'rename', placeId: id }), newWindowHint: placeShortcuts.openInNewWindow }),
  { kind: 'separator', id: 'close-separator' }, { id: 'close-place', label: 'Close place', shortcut: placeShortcuts.close, onSelect: () => shell.closePlace(current.id) }] : undefined;
 const stripToggle = sidebarHidden ? <RailToggle ref={sidebarToggle} placement="strip" collapsed onClick={toggleSidebar}/> : undefined;
 // Outer to inner: the place, then Next up, then focus history (its restore asks the shell to move).
 // The strip draws the frame pill and the banner from the Next up provider. A second pair here would leave the strip.
 return <PlacesShellProvider value={shell}><NextUpProvider controller={nextUpWindow}><FocusHistoryProvider wire={focusWire}><div className={`app-shell ${sidebarHidden ? 'sidebar-collapsed' : ''}`} data-tint={frameTint} data-providers="places next-up focus-history" data-focus={frame.focus || undefined} data-peek={frame.peek || undefined}>
  {frame.edges.top && <div className="shell-hotzone" data-edge="top" data-tauri-drag-region onPointerEnter={frame.enterTopEdge}/>}
  {frame.edges.left && <div className="shell-hotzone" data-edge="left" onPointerEnter={frame.enterLeftEdge} onPointerLeave={frame.leaveLeftEdge}/>}
  {narrow ? <dialog ref={drawer} className="sidebar-drawer" aria-label="Navigation" onCancel={() => setDrawerOpen(false)} onClose={() => {
   setDrawerOpen(false);
   // Explicit restoration also works where pointer clicks do not focus buttons.
   // ⌘K from the open drawer lands in the New-tab field; only a drawer closed with nothing else focused returns to its toggle.
   if (!document.activeElement || document.activeElement === document.body) sidebarToggle.current?.focus();
  }} onClick={event => {
   if (event.target !== event.currentTarget) return;
   const bounds = event.currentTarget.getBoundingClientRect();
   if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) setDrawerOpen(false);
  }}>{sidebar}</dialog> : sidebar}
  <main className="content-pane" inert={narrow && drawerOpen}>
   {page !== 'Workspace' && <header className="content-toolbar" data-tauri-drag-region>
    {stripToggle}
    <span className="page-title">{page}</span>
   </header>}
   <div className="workspace-page" hidden={page !== 'Workspace'}><Workspace key={shell.place} place={shell.place} placeTitle={current?.name} placeTint={current?.effectiveTint} arrival={shell.arrival}
    firstTurn={shell.place === 'now' ? undefined : firstTurn} placeMenu={placeMenu} placeSwitcher={sidebarHidden ? placeSwitcher(shell, rail, switcherRowContents) : undefined}
    onActivate={() => { navigate('Workspace'); }} enabled={page === 'Workspace'} leading={stripToggle}/></div>
   {import.meta.env.DEV && page === 'Design system' && <div className="content-card"><div className="page-content"><PageHeading>Less, but considered.</PageHeading><Text className="intro">Soft chrome. Native type. Space to focus.</Text><Surface direction="column"><SectionHeading>Surfaces</SectionHeading><div className="swatches">{['canvas','surface','overlay-surface','composer-surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Typography</SectionHeading><Text className="type-sample">The font your device calls home.</Text><Text>System sans for the interface. System monospace for code.</Text><CodeText>const workspace = "codeaf";</CodeText></Surface><Surface direction="column"><SectionHeading>Response typography</SectionHeading><Markdown>{'# A readable result\n\n## Findings\n\nUse `src/engine.ts` and `npm run check` without changing the interface font.\n\n### Next step\n\n**Emphasis**, lists and `inline code` use shared type.\n\n- One finding\n- Another finding\n\n| Item | State |\n| --- | --- |\n| Example | Ready |\n\n```ts\nconst ready = true;\n```'}</Markdown></Surface><Surface direction="column"><SectionHeading>Icon family</SectionHeading><Text>AnimateIcons · Lucide · one monochrome stroke style.</Text><div className="icon-specimens">{iconNames.map(name => <IconButton key={name} label={`${name} icon`} icon={name}/>)}</div></Surface><FoundationsSpecimen/><PrimitivesSpecimen/><ControlsSpecimen/><Surface direction="column"><SectionHeading>Work states</SectionHeading><Text>Still indicators. Full activity details live in tab previews.</Text><div className="control-specimens">{(['streaming','working','waiting','completed','stopped','failed','staged'] as const).map(phase=><div className="work-state-specimen" key={phase}><WorkStateIndicator phase={phase} label={`${phase} sample`}/><Text>{phase}</Text></div>)}</div></Surface><Surface direction="column"><SectionHeading>Menus and motion</SectionHeading><Text>Menus use the same quiet surfaces, focus, and keyboard controls.</Text><div className="control-specimens"><DropdownMenu label="Menu specimen" items={[{ id: 'workspace', label: 'Open workspace', icon: 'tab', onSelect: () => navigate('Workspace') }, { id: 'disabled', label: 'Unavailable action', disabled: true, onSelect: () => {} }]}><Button variant="raised">Open themed menu <Icon name="chevron" size="xs" motion="disclosure"/></Button></DropdownMenu><ContextMenu label="Context menu specimen" items={[{ id: 'workspace', label: 'Open workspace', onSelect: () => navigate('Workspace') }]}><Button variant="ghost">Right-click or Shift F10</Button></ContextMenu></div></Surface><TabsSpecimen/><GroupOfferSpecimen/><HistorySpecimen/><PreviewSpecimen/><GoToChooserSpecimen/><PlaceDialogsSpecimen/><ToastSpecimen/><FilesSpecimen/><NewTabSpecimen/><TerminalSpecimen/><WebAddressSpecimen/><RailSpecimen/><RailRowsSpecimen/><PlacesSpecimen/><ConversationSpecimens/><Surface direction="column"><SectionHeading>Spacing</SectionHeading><div className="spacing-specimens">{[1,2,3,4,6,8,12].map(n => <div key={n}><div className={`spacing-sample spacing-sample-${n}`}/><small>{design.foundation[`space-${n}` as keyof typeof design.foundation]}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Built-in care</SectionHeading><Text>Keyboard navigation, visible focus, reduced motion, and system appearance.</Text><Button variant="ghost" className="quiet-action" onClick={requestNewTabField}>Open the new-tab field <Icon name="arrow" size="xs" motion="directional"/></Button></Surface></div></div>}
  </main>
  <PlacesOverlays shell={shell}/>
  {/* The window's ONE toast region: a closed tab's Stop it and a place's Undo stand in the same stack. */}
  <ToastHost/>
 </div></FocusHistoryProvider></NextUpProvider></PlacesShellProvider>;
}
export default App;

import { TerminalSpecimen } from './features/terminal/TerminalSpecimen';
import { NewTabSpecimen } from './features/tabs/kinds/newtab/NewTabSpecimen';
import { FilesSpecimen } from './features/files/specimens/FilesSpecimen';
import { PreviewSpecimen } from './features/tabs/specimens/PreviewSpecimen';
import { useCallback, useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { checkEngine } from './lib/engine';
import { connectDesktopTabs } from './lib/desktopTabs';
import { Button, IconButton, Icon, PageHeading, SectionHeading, Text, CodeText, Markdown, Surface, ContextMenu, DropdownMenu, WorkStateIndicator, iconNames, type MenuEntry } from './components/ui';
import { CommandPalette } from './components/CommandPalette';
import design from './design/tokens.json';
import { useMediaQuery } from './design/useMediaQuery';
import './App.css';
import { Workspace } from './features/tabs/Workspace';
import { PlaceRail, type RailItem } from './features/shell/PlaceRail';
import { PlacesShellProvider, usePlacesShellController } from './features/places/shell/PlacesShell';
import { PlacesOverlays } from './features/places/shell/PlacesOverlays';
import { placeSwitcher, useAttentionNotices, usePlaceKeys, usePlaceRail, useWindowTint } from './features/places/shell/useShellPlaces';
import { GoToChooserSpecimen } from './features/places/shell/GoToChooserSpecimen';
import { PlaceDialogsSpecimen, ToastSpecimen } from './features/places/shell/PlaceDialogsSpecimen';
import { chatIdFromSessionFile } from './features/places/client';
import { homeMenu } from './features/places/place-actions';
import { placeShortcuts } from './design/keyboard';
import { TabsSpecimen } from './features/tabs/specimens/TabsSpecimen';
import { RailSpecimen } from './features/shell/specimens/RailSpecimen';
import { ControlsSpecimen } from './components/specimens/ControlsSpecimen';
import { ConversationSpecimens } from './features/conversation/specimens/ConversationSpecimens';
import { RailToggle } from './features/shell/RailToggle';
import { useShellFrame } from './features/shell/useShellFrame';
import { requestLeaveKind, requestOpenKind, useActiveHome, useActiveKind } from './features/shell/shellState';
import { shortcutLayer } from './design/keyboard';
import { useShortcuts } from './design/useShortcuts';

/** Settings is a tab of the workspace (its own kind), so it is not a page here: its rail item opens that tab. */
type Page = 'Workspace' | 'Activity' | 'Design system';
const commands = ['Workspace', 'Activity', 'Settings', 'Design system'] as const;
const desktop = isTauri();
const mac = desktop && /Mac/.test(navigator.platform);
document.documentElement.dataset.environment = mac ? 'mac-desktop' : desktop ? 'desktop' : 'browser';
function App() {
 useEffect(connectDesktopTabs, []);
 const [page, setPage] = useState<Page>('Workspace');
 const [palette, setPalette] = useState(false);
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
 function openPalette() { setDrawerOpen(false); setPalette(true); }
 function navigate(next: Page) { setPage(next); setDrawerOpen(false); }
 function toggleSidebar() { if (narrow) setDrawerOpen(open => !open); else frame.toggleRail(); }
 const [engine, setEngine] = useState('Not checked');
 const [busy, setBusy] = useState(false);
 // The app layer of the shell's one shortcut registry (design/keyboard.ts): the rail, Focus mode and the palette work on every page.
 useShortcuts(shortcutLayer.app, shortcut => {
  if (shortcut.id === 'palette') { setDrawerOpen(false); setPalette(p => !p); return true; }
  if (shortcut.id === 'rail') { toggleSidebar(); return true; }
  if (shortcut.id === 'focus') { frame.toggleFocus(); return true; }
  if (shortcut.id === 'settings') { requestOpenKind('settings'); return true; }
  return false;
 });
 function select(command: string) { if (command === 'Settings') requestOpenKind('settings'); else navigate(command as Page); }
 async function health() {
  setBusy(true);
  try { const h = await checkEngine(); setEngine(`${h.status} · v${h.version} · ${h.platform}`); }
  catch (e) { setEngine(String(e instanceof Error ? e.message : e)); }
  finally { setBusy(false); }
 }
 const settingsOpen = page === 'Workspace' && activeKind === 'settings';
 const shell = usePlacesShellController();
 const activeHome = useActiveHome();
 // Every place verb lands in the workspace: going somewhere from another page brings the workspace back.
 const enterWorkspace = useCallback(() => { setDrawerOpen(false); setPage('Workspace'); if (activeKind === 'settings') requestLeaveKind('settings'); }, [activeKind]);
 usePlaceKeys(shell, enterWorkspace);
 useWindowTint(shell);
 const attention = useAttentionNotices();
 const appItems: RailItem[] = [
  { label: 'Activity', icon: 'activity', active: page === 'Activity', onSelect: () => navigate('Activity') },
  { label: 'Settings', icon: 'sliders', active: settingsOpen, onSelect: () => select('Settings') },
  { label: 'Design system', icon: 'grid', active: page === 'Design system', onSelect: () => navigate('Design system') },
 ];
 const rail = usePlaceRail(shell, {
  inert: narrow ? !drawerOpen : sidebarHidden && frame.peek !== 'rail', paletteOpen: palette, peeking: frame.peek === 'rail',
  onToggle: () => narrow ? setDrawerOpen(false) : toggleSidebar(), onSearch: openPalette, appItems,
  onWorkspace: page === 'Workspace' && !settingsOpen && activeKind !== 'inbox', activeHome, onEnterWorkspace: enterWorkspace,
  inbox: { count: attention.items.filter(item => item.kind === 'needsYou').length, active: page === 'Workspace' && activeKind === 'inbox' },
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
 const placeMenu: MenuEntry[] | undefined = current ? [...homeMenu({ id: current.id, name: current.name, tint: current.effectiveTint, pinned: current.pinned }, {
  goToInNewWindow: id => shell.goToInNewWindow(id),
  pin: id => void shell.write(`Pinned “${current.name}” to the rail`, () => shell.client.pinPlace(id), { subject: current.name }).catch(shell.warn),
  unpin: id => void shell.write(`Unpinned “${current.name}”`, () => shell.client.unpinPlace(id), { subject: current.name }).catch(shell.warn),
  setTint: (id, next) => void shell.write(`Changed the tint of “${current.name}”`, () => shell.client.updatePlace(id, { tint: next }), { subject: current.name }).catch(shell.warn),
  chooseAnotherParent: id => shell.openChooser({ kind: 'parent', placeId: id, placeName: current.name }),
  chooseMergeTarget: id => shell.openChooser({ kind: 'merge', placeId: id, placeName: current.name }),
 }, { canRename: true, startRename: id => shell.openDialog({ kind: 'rename', placeId: id }), newWindowHint: placeShortcuts.openInNewWindow }),
  { kind: 'separator', id: 'close-separator' }, { id: 'close-place', label: 'Close place', shortcut: placeShortcuts.close, onSelect: () => shell.closePlace(current.id) }] : undefined;
 const stripToggle = sidebarHidden ? <RailToggle ref={sidebarToggle} placement="strip" collapsed onClick={toggleSidebar}/> : undefined;
 return <PlacesShellProvider value={shell}><div className={`app-shell ${sidebarHidden ? 'sidebar-collapsed' : ''}`} data-focus={frame.focus || undefined} data-peek={frame.peek || undefined}>
  {frame.edges.top && <div className="shell-hotzone" data-edge="top" data-tauri-drag-region onPointerEnter={frame.enterTopEdge}/>}
  {frame.edges.left && <div className="shell-hotzone" data-edge="left" onPointerEnter={frame.enterLeftEdge} onPointerLeave={frame.leaveLeftEdge}/>}
  {narrow ? <dialog ref={drawer} className="sidebar-drawer" aria-label="Navigation" onCancel={() => setDrawerOpen(false)} onClose={() => {
   setDrawerOpen(false);
   // Explicit restoration also works where pointer clicks do not focus buttons.
   if (!palette) sidebarToggle.current?.focus();
  }} onClick={event => {
   if (event.target !== event.currentTarget) return;
   const bounds = event.currentTarget.getBoundingClientRect();
   if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) setDrawerOpen(false);
  }}>{sidebar}</dialog> : sidebar}
  <main className="content-pane" inert={narrow && drawerOpen}>
   {page !== 'Workspace' && <header className="content-toolbar" data-tauri-drag-region>
    {stripToggle}
    <span className="page-title">{page}</span>
    <IconButton label="Search commands" icon="search" onClick={openPalette}/>
   </header>}
   <div className="workspace-page" hidden={page !== 'Workspace'}><Workspace key={shell.place} place={shell.place} placeTitle={current?.name} placeTint={current?.effectiveTint} arrival={shell.arrival}
    firstTurn={shell.place === 'now' ? undefined : firstTurn} placeMenu={placeMenu} placeSwitcher={sidebarHidden ? placeSwitcher(shell, rail) : undefined} onError={shell.warn}
    onActivate={() => { setPalette(false); navigate('Workspace'); }} enabled={page === 'Workspace'} leading={stripToggle}/></div>
   {page === 'Activity' && <div className="content-card"><div className="page-content"><PageHeading>Activity</PageHeading><Text className="intro">Your workspace is quiet. No sessions yet.</Text><Surface><div><SectionHeading>Local engine</SectionHeading><Text role="status">{engine}</Text></div><Button variant="quiet" loading={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</Button></Surface></div></div>}
   {page === 'Design system' && <div className="content-card"><div className="page-content"><PageHeading>Less, but considered.</PageHeading><Text className="intro">Soft chrome. Native type. Space to focus.</Text><Surface direction="column"><SectionHeading>Surfaces</SectionHeading><div className="swatches">{['canvas','surface','overlay-surface','composer-surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Typography</SectionHeading><Text className="type-sample">The font your device calls home.</Text><Text>System sans for the interface. System monospace for code.</Text><CodeText>const workspace = "codeaf";</CodeText></Surface><Surface direction="column"><SectionHeading>Response typography</SectionHeading><Markdown>{'# A readable result\n\n## Findings\n\nUse `src/engine.ts` and `npm run check` without changing the interface font.\n\n### Next step\n\n**Emphasis**, lists and `inline code` use shared type.\n\n- One finding\n- Another finding\n\n| Item | State |\n| --- | --- |\n| Example | Ready |\n\n```ts\nconst ready = true;\n```'}</Markdown></Surface><Surface direction="column"><SectionHeading>Icon family</SectionHeading><Text>AnimateIcons · Lucide · one monochrome stroke style.</Text><div className="icon-specimens">{iconNames.map(name => <IconButton key={name} label={`${name} icon`} icon={name}/>)}</div></Surface><ControlsSpecimen/><Surface direction="column"><SectionHeading>Work states</SectionHeading><Text>Still indicators. Full activity details live in tab previews.</Text><div className="control-specimens">{(['streaming','working','waiting','completed','stopped','failed','staged'] as const).map(phase=><div className="work-state-specimen" key={phase}><WorkStateIndicator phase={phase} label={`${phase} sample`}/><Text>{phase}</Text></div>)}</div></Surface><Surface direction="column"><SectionHeading>Menus and motion</SectionHeading><Text>Menus use the same quiet surfaces, focus, and keyboard controls.</Text><div className="control-specimens"><DropdownMenu label="Menu specimen" items={[{ id: 'workspace', label: 'Open workspace', icon: 'tab', onSelect: () => navigate('Workspace') }, { id: 'disabled', label: 'Unavailable action', disabled: true, onSelect: () => {} }]}><Button variant="raised">Open themed menu <Icon name="chevron" size="xs" motion="disclosure"/></Button></DropdownMenu><ContextMenu label="Context menu specimen" items={[{ id: 'workspace', label: 'Open workspace', onSelect: () => navigate('Workspace') }]}><Button variant="ghost">Right-click or Shift F10</Button></ContextMenu></div></Surface><TabsSpecimen/><PreviewSpecimen/><GoToChooserSpecimen/><PlaceDialogsSpecimen/><ToastSpecimen/><FilesSpecimen/><NewTabSpecimen/><TerminalSpecimen/><RailSpecimen/><ConversationSpecimens/><Surface direction="column"><SectionHeading>Spacing</SectionHeading><div className="spacing-specimens">{[1,2,3,4,6,8,12].map(n => <div key={n}><div className={`spacing-sample spacing-sample-${n}`}/><small>{design.foundation[`space-${n}` as keyof typeof design.foundation]}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Built-in care</SectionHeading><Text>Keyboard navigation, visible focus, reduced motion, and system appearance.</Text><Button variant="ghost" className="quiet-action" onClick={openPalette}>Open command palette <Icon name="arrow" size="xs" motion="directional"/></Button></Surface></div></div>}
  </main>
  <CommandPalette open={palette} onClose={() => setPalette(false)} commands={commands} onSelect={select}/>
  <PlacesOverlays shell={shell}/>
 </div></PlacesShellProvider>;
}
export default App;

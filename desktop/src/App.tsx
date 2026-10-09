import { useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { checkEngine } from './lib/engine';
import { connectDesktopTabs } from './lib/desktopTabs';
import { Button, IconButton, Icon, PageHeading, SectionHeading, Text, CodeText, Markdown, Surface, ContextMenu, DropdownMenu, WorkStateIndicator, iconNames } from './components/ui';
import { CommandPalette } from './components/CommandPalette';
import design from './design/tokens.json';
import { useMediaQuery } from './design/useMediaQuery';
import './App.css';
import { Workspace } from './features/tabs/Workspace';
import { Rail } from './features/shell/Rail';
import { TabsSpecimen } from './features/tabs/specimens/TabsSpecimen';
import { TerminalSpecimen } from './features/terminal/TerminalSpecimen';
import { ControlsSpecimen } from './components/specimens/ControlsSpecimen';
import { ConversationSpecimens } from './features/conversation/specimens/ConversationSpecimens';
import { SettingsPage } from './features/settings';

type Page = 'Workspace' | 'Activity' | 'Settings' | 'Design system';
const pages: Page[] = ['Workspace', 'Activity', 'Settings', 'Design system'];
const desktop = isTauri();
const mac = desktop && /Mac/.test(navigator.platform);
document.documentElement.dataset.environment = mac ? 'mac-desktop' : desktop ? 'desktop' : 'browser';
function App() {
 useEffect(connectDesktopTabs, []);
 const [page, setPage] = useState<Page>('Workspace');
 const [palette, setPalette] = useState(false);
 const [collapsed, setCollapsed] = useState(false);
 const narrow = useMediaQuery(`(max-width: ${design.breakpoints.small}px)`);
 const [drawerOpen, setDrawerOpen] = useState(false);
 const drawer = useRef<HTMLDialogElement>(null);
 const sidebarToggle = useRef<HTMLButtonElement>(null);
 const sidebarHidden = narrow || collapsed;
 useEffect(() => {
  if (narrow && drawerOpen) { if (!drawer.current?.open) drawer.current?.showModal(); }
  else { drawer.current?.close(); setDrawerOpen(false); }
 }, [narrow, drawerOpen]);
 function openPalette() { setDrawerOpen(false); setPalette(true); }
 function navigate(next: Page) { setPage(next); setDrawerOpen(false); }
 function toggleSidebar() { if (narrow) setDrawerOpen(open => !open); else setCollapsed(value => !value); }
 const [engine, setEngine] = useState('Not checked');
 const [busy, setBusy] = useState(false);
 useEffect(() => {
  const onKey = (e: KeyboardEvent) => {
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setDrawerOpen(false); setPalette(p => !p); }
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') { e.preventDefault(); if (narrow) setDrawerOpen(p => !p); else setCollapsed(p => !p); }
  };
  window.addEventListener('keydown', onKey);
  return () => window.removeEventListener('keydown', onKey);
 }, [narrow]);
 async function health() {
  setBusy(true);
  try { const h = await checkEngine(); setEngine(`${h.status} · v${h.version} · ${h.platform}`); }
  catch (e) { setEngine(String(e instanceof Error ? e.message : e)); }
  finally { setBusy(false); }
 }
 const navIcons = ['code', 'activity', 'sliders', 'grid'] as const;
 const sidebar = <Rail pages={pages} page={page} icons={navIcons} inert={narrow ? !drawerOpen : collapsed} paletteOpen={palette} onNavigate={navigate} onHide={() => narrow ? setDrawerOpen(false) : setCollapsed(true)} onSearch={openPalette}/>;
 return <div className={`app-shell ${sidebarHidden ? 'sidebar-collapsed' : ''}`}>
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
    {sidebarHidden && <IconButton ref={sidebarToggle} label="Show sidebar" icon="sidebar" title="Show sidebar (⌘/Ctrl B)" onClick={toggleSidebar}/>}
    <span className="page-title">{page}</span>
    <IconButton label="Search commands" icon="search" onClick={openPalette}/>
   </header>}
   <div className="workspace-page" hidden={page !== 'Workspace'}><Workspace onActivate={() => { setPalette(false); navigate('Workspace'); }} enabled={page === 'Workspace'} leading={sidebarHidden ? <IconButton ref={sidebarToggle} label="Show sidebar" icon="sidebar" title="Show sidebar (⌘/Ctrl B)" onClick={toggleSidebar}/> : undefined}/></div>
   {page === 'Activity' && <div className="content-card"><div className="page-content"><PageHeading>Activity</PageHeading><Text className="intro">Your workspace is quiet. No sessions yet.</Text><Surface><div><SectionHeading>Local engine</SectionHeading><Text role="status">{engine}</Text></div><Button variant="quiet" loading={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</Button></Surface></div></div>}
   {page === 'Settings' && <div className="content-card"><div className="settings-scroll"><SettingsPage/></div></div>}
   {page === 'Design system' && <div className="content-card"><div className="page-content"><PageHeading>Less, but considered.</PageHeading><Text className="intro">Soft chrome. Native type. Space to focus.</Text><Surface direction="column"><SectionHeading>Surfaces</SectionHeading><div className="swatches">{['canvas','surface','overlay-surface','composer-surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Typography</SectionHeading><Text className="type-sample">The font your device calls home.</Text><Text>System sans for the interface. System monospace for code.</Text><CodeText>const workspace = "codeaf";</CodeText></Surface><Surface direction="column"><SectionHeading>Response typography</SectionHeading><Markdown>{'# A readable result\n\n## Findings\n\nUse `src/engine.ts` and `npm run check` without changing the interface font.\n\n### Next step\n\n**Emphasis**, lists and `inline code` use shared type.\n\n- One finding\n- Another finding\n\n| Item | State |\n| --- | --- |\n| Example | Ready |\n\n```ts\nconst ready = true;\n```'}</Markdown></Surface><Surface direction="column"><SectionHeading>Icon family</SectionHeading><Text>AnimateIcons · Lucide · one monochrome stroke style.</Text><div className="icon-specimens">{iconNames.map(name => <IconButton key={name} label={`${name} icon`} icon={name}/>)}</div></Surface><ControlsSpecimen/><Surface direction="column"><SectionHeading>Work states</SectionHeading><Text>Still indicators. Full activity details live in tab previews.</Text><div className="control-specimens">{(['streaming','working','waiting','completed','stopped','failed','staged'] as const).map(phase=><div className="work-state-specimen" key={phase}><WorkStateIndicator phase={phase} label={`${phase} sample`}/><Text>{phase}</Text></div>)}</div></Surface><Surface direction="column"><SectionHeading>Menus and motion</SectionHeading><Text>Menus use the same quiet surfaces, focus, and keyboard controls.</Text><div className="control-specimens"><DropdownMenu label="Menu specimen" items={[{ id: 'workspace', label: 'Open workspace', icon: 'tab', onSelect: () => navigate('Workspace') }, { id: 'disabled', label: 'Unavailable action', disabled: true, onSelect: () => {} }]}><Button variant="raised">Open themed menu <Icon name="chevron" size="xs" motion="disclosure"/></Button></DropdownMenu><ContextMenu label="Context menu specimen" items={[{ id: 'workspace', label: 'Open workspace', onSelect: () => navigate('Workspace') }]}><Button variant="ghost">Right-click or Shift F10</Button></ContextMenu></div></Surface><TabsSpecimen/><TerminalSpecimen/><ConversationSpecimens/><Surface direction="column"><SectionHeading>Spacing</SectionHeading><div className="spacing-specimens">{[1,2,3,4,6,8,12].map(n => <div key={n}><div className={`spacing-sample spacing-sample-${n}`}/><small>{design.foundation[`space-${n}` as keyof typeof design.foundation]}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Built-in care</SectionHeading><Text>Keyboard navigation, visible focus, reduced motion, and system appearance.</Text><Button variant="ghost" className="quiet-action" onClick={openPalette}>Open command palette <Icon name="arrow" size="xs" motion="directional"/></Button></Surface></div></div>}
  </main>
  <CommandPalette open={palette} onClose={() => setPalette(false)} commands={pages} onSelect={command => setPage(command as Page)}/>
 </div>;
}
export default App;

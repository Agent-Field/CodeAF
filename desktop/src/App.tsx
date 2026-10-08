import { useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { checkEngine } from './lib/engine';
import { BrandMark, Button, IconButton, Icon, NavigationItem, SidebarAction, PageHeading, SectionHeading, Text, CodeText, Surface, Separator, ThemeSelect, KeyboardShortcut, iconNames } from './components/ui';
import { CommandPalette } from './components/CommandPalette';
import design from './design/tokens.json';
import { useMediaQuery } from './design/useMediaQuery';
import './App.css';

type Page = 'Workspace' | 'Activity' | 'Design system';
const pages: Page[] = ['Workspace', 'Activity', 'Design system'];
const desktop = isTauri();
const mac = desktop && /Mac/.test(navigator.platform);
document.documentElement.dataset.environment = mac ? 'mac-desktop' : desktop ? 'desktop' : 'browser';
function App() {
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
 const navIcons = ['code', 'activity', 'grid'] as const;
 const sidebar = <aside className="sidebar" aria-label="Main navigation" inert={narrow ? !drawerOpen : collapsed}>
   <div className="sidebar-toolbar" data-tauri-drag-region>
    <IconButton label="Hide sidebar" icon="sidebar" title="Hide sidebar (⌘/Ctrl B)" onClick={() => narrow ? setDrawerOpen(false) : setCollapsed(true)}/>
   </div>
   <SidebarAction variant="address" aria-haspopup="dialog" aria-expanded={palette} onClick={openPalette}><BrandMark/><span>codeaf</span><Icon name="search" size="xs"/></SidebarAction>
   <div className="favorites" aria-label="Quick navigation">{pages.map((p, i) => <SidebarAction variant="favorite" active={page === p} key={p} aria-label={`Open ${p}`} title={p} onClick={() => navigate(p)}><Icon name={navIcons[i]} size="lg"/></SidebarAction>)}</div>
   <div className="space-name">Personal space</div>
   <nav>{pages.map((p, i) => <NavigationItem key={p} icon={navIcons[i]} active={page === p} onClick={() => navigate(p)}>{p}</NavigationItem>)}</nav>
   <Separator className="sidebar-divider"/>
   <SidebarAction variant="new" onClick={openPalette}><Icon name="plus" size="sm"/><span>Find anything</span><KeyboardShortcut command="K"/></SidebarAction>
   <div className="sidebar-bottom"><ThemeSelect/><IconButton label="Quick commands" title="Command palette" icon="plus" onClick={openPalette}/></div>
  </aside>;
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
   <header className="content-toolbar" data-tauri-drag-region>
    {sidebarHidden && <IconButton ref={sidebarToggle} label="Show sidebar" icon="sidebar" title="Show sidebar (⌘/Ctrl B)" onClick={toggleSidebar}/>}
    <span className="page-title">{page}</span>
    <IconButton label="Search commands" icon="search" onClick={openPalette}/>
   </header>
   {page === 'Workspace' && <div className="workspace-empty"><div className="empty-content"><BrandMark size="hero"/><PageHeading>A space for what’s next.</PageHeading><Text>Your projects and conversations will live here.</Text><Button variant="quiet" className="quiet-action" onClick={() => setPage('Design system')}>Explore the foundation <Icon name="arrow" size="xs" motion="directional"/></Button></div><span className="workspace-caption">codeaf · A quieter way to build</span></div>}
   {page === 'Activity' && <div className="page-content"><PageHeading>Activity</PageHeading><Text className="intro">Your workspace is quiet. No sessions yet.</Text><Surface><div><SectionHeading>Local engine</SectionHeading><Text role="status">{engine}</Text></div><Button variant="secondary" loading={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</Button></Surface></div>}
   {page === 'Design system' && <div className="page-content"><PageHeading>Less, but considered.</PageHeading><Text className="intro">Soft chrome. Native type. Space to focus.</Text><Surface direction="column"><SectionHeading>Surfaces</SectionHeading><div className="swatches">{['canvas','surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Typography</SectionHeading><Text className="type-sample">The font your device calls home.</Text><Text>System sans for the interface. System monospace for code.</Text><CodeText>const workspace = "codeaf";</CodeText></Surface><Surface direction="column"><SectionHeading>Icon family</SectionHeading><Text>AnimateIcons · Lucide · one monochrome stroke style.</Text><div className="icon-specimens">{iconNames.map(name => <IconButton key={name} label={`${name} icon`} icon={name}/>)}</div></Surface><Surface direction="column"><SectionHeading>Shared controls</SectionHeading><div className="control-specimens"><Button variant="primary">Primary action</Button><Button variant="secondary">Secondary action</Button><Button>Quiet action</Button><Button variant="secondary" disabled>Disabled</Button></div></Surface><Surface direction="column"><SectionHeading>Spacing</SectionHeading><div className="spacing-specimens">{[1,2,3,4,6,8,12].map(n => <div key={n}><div className={`spacing-sample spacing-sample-${n}`}/><small>{design.foundation[`space-${n}` as keyof typeof design.foundation]}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Built-in care</SectionHeading><Text>Keyboard navigation, visible focus, reduced motion, and system appearance.</Text><Button variant="quiet" className="quiet-action" onClick={openPalette}>Open command palette <Icon name="arrow" size="xs" motion="directional"/></Button></Surface></div>}
  </main>
  <CommandPalette open={palette} onClose={() => setPalette(false)} commands={pages} onSelect={command => setPage(command as Page)}/>
 </div>;
}
export default App;

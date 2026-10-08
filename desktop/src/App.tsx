import { useEffect, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { checkEngine } from './lib/engine';
import { BrandMark, Button, IconButton, Icon, NavigationItem, SidebarAction, PageHeading, SectionHeading, Text, CodeText, Surface, Separator, ThemeSelect, KeyboardShortcut, iconNames } from './components/ui';
import { CommandPalette } from './components/CommandPalette';
import design from './design/tokens.json';
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
 const [engine, setEngine] = useState('Not checked');
 const [busy, setBusy] = useState(false);
 useEffect(() => {
  const onKey = (e: KeyboardEvent) => {
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); }
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') { e.preventDefault(); setCollapsed(p => !p); }
  };
  window.addEventListener('keydown', onKey);
  return () => window.removeEventListener('keydown', onKey);
 }, []);
 async function health() {
  setBusy(true);
  try { const h = await checkEngine(); setEngine(`${h.status} · v${h.version} · ${h.platform}`); }
  catch (e) { setEngine(String(e instanceof Error ? e.message : e)); }
  finally { setBusy(false); }
 }
 const navIcons = ['code', 'activity', 'grid'] as const;
 return <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
  <aside className="sidebar" aria-label="Main navigation" inert={collapsed}>
   <div className="sidebar-toolbar" data-tauri-drag-region>
    <IconButton label="Hide sidebar" icon="sidebar" title="Hide sidebar (⌘/Ctrl B)" onClick={() => setCollapsed(true)}/>
   </div>
   <SidebarAction variant="address" aria-haspopup="dialog" aria-expanded={palette} onClick={() => setPalette(true)}><BrandMark/><span>CodeAF</span><Icon name="search" size="xs"/></SidebarAction>
   <div className="favorites" aria-label="Quick navigation">{pages.map((p, i) => <SidebarAction variant="favorite" active={page === p} key={p} aria-label={`Open ${p}`} title={p} onClick={() => setPage(p)}><Icon name={navIcons[i]} size="lg"/></SidebarAction>)}</div>
   <div className="space-name">Personal space</div>
   <nav>{pages.map((p, i) => <NavigationItem key={p} icon={navIcons[i]} active={page === p} onClick={() => setPage(p)}>{p}</NavigationItem>)}</nav>
   <Separator className="sidebar-divider"/>
   <SidebarAction variant="new" onClick={() => setPalette(true)}><Icon name="plus" size="sm"/><span>Find anything</span><KeyboardShortcut command="K"/></SidebarAction>
   <div className="sidebar-bottom"><ThemeSelect/><IconButton label="Quick commands" title="Command palette" icon="plus" onClick={() => setPalette(true)}/></div>
  </aside>
  <main className="content-pane">
   <header className="content-toolbar" data-tauri-drag-region>
    {collapsed && <IconButton label="Show sidebar" icon="sidebar" title="Show sidebar (⌘/Ctrl B)" onClick={() => setCollapsed(false)}/>}
    <span className="page-title">{page}</span>
    <IconButton label="Search commands" icon="search" onClick={() => setPalette(true)}/>
   </header>
   {page === 'Workspace' && <div className="workspace-empty"><div className="empty-content"><BrandMark size="hero"/><PageHeading>A space for what’s next.</PageHeading><Text>Your projects and conversations will live here.</Text><Button variant="quiet" className="quiet-action" onClick={() => setPage('Design system')}>Explore the foundation <Icon name="arrow" size="xs" motion="directional"/></Button></div><span className="workspace-caption">CodeAF · A quieter way to build</span></div>}
   {page === 'Activity' && <div className="page-content"><PageHeading>Activity</PageHeading><Text className="intro">Your workspace is quiet. No sessions yet.</Text><Surface><div><SectionHeading>Local engine</SectionHeading><Text role="status">{engine}</Text></div><Button variant="secondary" loading={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</Button></Surface></div>}
   {page === 'Design system' && <div className="page-content"><PageHeading>Less, but considered.</PageHeading><Text className="intro">Soft chrome. Native type. Space to focus.</Text><Surface direction="column"><SectionHeading>Surfaces</SectionHeading><div className="swatches">{['canvas','surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Typography</SectionHeading><Text className="type-sample">The font your device calls home.</Text><Text>System sans for the interface. System monospace for code.</Text><CodeText>const workspace = "CodeAF";</CodeText></Surface><Surface direction="column"><SectionHeading>Icon family</SectionHeading><Text>AnimateIcons · Lucide · one monochrome stroke style.</Text><div className="icon-specimens">{iconNames.map(name => <IconButton key={name} label={`${name} icon`} icon={name}/>)}</div></Surface><Surface direction="column"><SectionHeading>Shared controls</SectionHeading><div className="control-specimens"><Button variant="primary">Primary action</Button><Button variant="secondary">Secondary action</Button><Button>Quiet action</Button><Button variant="secondary" disabled>Disabled</Button></div></Surface><Surface direction="column"><SectionHeading>Spacing</SectionHeading><div className="spacing-specimens">{[1,2,3,4,6,8,12].map(n => <div key={n}><div className={`spacing-sample spacing-sample-${n}`}/><small>{design.foundation[`space-${n}` as keyof typeof design.foundation]}</small></div>)}</div></Surface><Surface direction="column"><SectionHeading>Built-in care</SectionHeading><Text>Keyboard navigation, visible focus, reduced motion, and system appearance.</Text><Button variant="quiet" className="quiet-action" onClick={() => setPalette(true)}>Open command palette <Icon name="arrow" size="xs" motion="directional"/></Button></Surface></div>}
  </main>
  <CommandPalette open={palette} onClose={() => setPalette(false)} commands={pages} onSelect={command => setPage(command as Page)}/>
 </div>;
}
export default App;

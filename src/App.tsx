import { useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { checkEngine } from './lib/engine';
import './App.css';

type Theme = 'system' | 'light' | 'dark';
type Page = 'Workspace' | 'Activity' | 'Design system';
const pages: Page[] = ['Workspace', 'Activity', 'Design system'];
const desktop = isTauri();
const mac = desktop && /Mac/.test(navigator.platform);
document.documentElement.dataset.environment = mac ? 'mac-desktop' : desktop ? 'desktop' : 'browser';
function savedTheme(): Theme {
 try { const t = localStorage.getItem('codeaf-theme'); return t === 'light' || t === 'dark' ? t : 'system'; } catch { return 'system'; }
}
function Icon({ name }: { name: 'sidebar' | 'plus' | 'search' | 'code' | 'activity' | 'grid' | 'settings' | 'arrow' }) {
 const paths = {
  sidebar: <><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/></>,
  plus: <path d="M12 5v14M5 12h14"/>,
  search: <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4 4"/></>,
  code: <><path d="m8 7-5 5 5 5m8-10 5 5-5 5m-3-13-2 16"/></>,
  activity: <><path d="M3 12h4l3-7 4 14 3-7h4"/></>,
  grid: <><rect x="4" y="4" width="6" height="6" rx="1"/><rect x="14" y="4" width="6" height="6" rx="1"/><rect x="4" y="14" width="6" height="6" rx="1"/><rect x="14" y="14" width="6" height="6" rx="1"/></>,
  settings: <><circle cx="12" cy="12" r="3"/><path d="m9 3-1 3-3 1-2 3 2 2-1 3 2 3 3-1 2 3h3l1-3 3-1 2-3-2-2 1-3-2-3-3 1-2-3Z"/></>,
  arrow: <path d="M5 12h14m-6-6 6 6-6 6"/>,
 };
 return <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
function App() {
 const [theme, setTheme] = useState<Theme>(savedTheme);
 const [page, setPage] = useState<Page>('Workspace');
 const [palette, setPalette] = useState(false);
 const [query, setQuery] = useState('');
 const [collapsed, setCollapsed] = useState(false);
 const [engine, setEngine] = useState('Not checked');
 const [busy, setBusy] = useState(false);
 const dialog = useRef<HTMLDialogElement>(null);
 const search = useRef<HTMLInputElement>(null);
 const lastFocused = useRef<HTMLElement | null>(null);
 useEffect(() => {
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem('codeaf-theme', theme); } catch { /* Appearance still works without storage. */ }
  if (desktop) void getCurrentWindow().setTheme(theme === 'system' ? null : theme).catch(console.error);
 }, [theme]);
 useEffect(() => {
  const onKey = (e: KeyboardEvent) => {
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); }
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') { e.preventDefault(); setCollapsed(p => !p); }
  };
  window.addEventListener('keydown', onKey);
  return () => window.removeEventListener('keydown', onKey);
 }, []);
 useEffect(() => {
  if (palette) {
   lastFocused.current = document.activeElement as HTMLElement;
   dialog.current?.showModal(); search.current?.focus();
  } else { dialog.current?.close(); setQuery(''); }
 }, [palette]);
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
    <button className="icon-button" aria-label="Hide sidebar" title="Hide sidebar (⌘/Ctrl B)" onClick={() => setCollapsed(true)}><Icon name="sidebar"/></button>
   </div>
   <button className="address-field" onClick={() => setPalette(true)}><span className="mini-mark">c</span><span>CodeAF</span><Icon name="search"/></button>
   <div className="favorites" aria-label="Quick navigation">{pages.map((p, i) => <button key={p} aria-label={`Open ${p}`} title={p} onClick={() => setPage(p)}><Icon name={navIcons[i]}/></button>)}</div>
   <div className="space-name">Personal space</div>
   <nav>{pages.map((p, i) => <button key={p} className={`nav-item ${page === p ? 'active' : ''}`} aria-current={page === p ? 'page' : undefined} onClick={() => setPage(p)}><Icon name={navIcons[i]}/><span>{p}</span></button>)}</nav>
   <div className="sidebar-divider"/>
   <button className="new-item" onClick={() => setPalette(true)}><Icon name="plus"/><span>Find anything</span><kbd>{/Mac/.test(navigator.platform) ? '⌘ K' : 'Ctrl K'}</kbd></button>
   <div className="sidebar-bottom"><label className="theme-control"><Icon name="settings"/><span className="sr-only">Theme</span><select aria-label="Theme" value={theme} onChange={e => setTheme(e.target.value as Theme)}><option value="system">System appearance</option><option value="light">Light appearance</option><option value="dark">Dark appearance</option></select></label><button className="icon-button" aria-label="Quick commands" title="Command palette" onClick={() => setPalette(true)}><Icon name="plus"/></button></div>
  </aside>
  <main className="content-pane">
   <header className="content-toolbar" data-tauri-drag-region>
    {collapsed && <button className="icon-button" aria-label="Show sidebar" title="Show sidebar (⌘/Ctrl B)" onClick={() => setCollapsed(false)}><Icon name="sidebar"/></button>}
    <span className="page-title">{page}</span>
    <button className="icon-button" aria-label="Search commands" onClick={() => setPalette(true)}><Icon name="search"/></button>
   </header>
   {page === 'Workspace' && <div className="workspace-empty"><div className="empty-content"><span className="empty-mark" aria-hidden="true">c</span><h1>A space for what’s next.</h1><p>Your projects and conversations will live here.</p><button className="quiet-action" onClick={() => setPage('Design system')}>Explore the foundation <Icon name="arrow"/></button></div><span className="workspace-caption">CodeAF · A quieter way to build</span></div>}
   {page === 'Activity' && <div className="page-content"><h1>Activity</h1><p className="intro">Your workspace is quiet. No sessions yet.</p><section className="settings-section"><div><h2>Local engine</h2><p role="status">{engine}</p></div><button className="secondary-button" disabled={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</button></section></div>}
   {page === 'Design system' && <div className="page-content"><h1>Less, but considered.</h1><p className="intro">Soft chrome. Native type. Space to focus.</p><section className="settings-section vertical"><h2>Surfaces</h2><div className="swatches">{['canvas','surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div></section><section className="settings-section vertical"><h2>Typography</h2><p className="type-sample">The font your device calls home.</p><p>System sans for the interface. System monospace for code.</p></section><section className="settings-section vertical"><h2>Built-in care</h2><p>Keyboard navigation, visible focus, reduced motion, and system appearance.</p><button className="quiet-action" onClick={() => setPalette(true)}>Open command palette <Icon name="arrow"/></button></section></div>}
  </main>
  <dialog ref={dialog} className="command-palette" onCancel={() => setPalette(false)} onClose={() => { setPalette(false); lastFocused.current?.focus(); }} aria-label="Command palette"><div className="palette-search"><Icon name="search"/><input ref={search} aria-label="Search commands" placeholder="Search your space…" value={query} onChange={e => setQuery(e.target.value)}/><button className="palette-close" onClick={() => setPalette(false)} aria-label="Close command palette"><kbd>esc</kbd></button></div><div className="commands">{pages.filter(p => p.toLowerCase().includes(query.toLowerCase())).map(p => <button key={p} onClick={() => {setPage(p);setPalette(false);}}>Go to {p}<Icon name="arrow"/></button>)}{!pages.some(p => p.toLowerCase().includes(query.toLowerCase())) && <p className="no-results">No matching commands</p>}</div></dialog>
 </div>;
}
export default App;

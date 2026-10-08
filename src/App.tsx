import { useEffect, useRef, useState } from 'react';
import { checkEngine } from './lib/engine';
import './App.css';

type Theme = 'system' | 'light' | 'dark';
type Page = 'Workspace' | 'Activity' | 'Design system';
const pages: Page[] = ['Workspace', 'Activity', 'Design system'];
function savedTheme(): Theme {
 try { const t = localStorage.getItem('codeaf-theme'); return t === 'light' || t === 'dark' ? t : 'system'; } catch { return 'system'; }
}
function App() {
 const [theme, setTheme] = useState<Theme>(savedTheme);
 const [page, setPage] = useState<Page>('Workspace');
 const [palette, setPalette] = useState(false);
 const [query, setQuery] = useState('');
 const [engine, setEngine] = useState('Not checked');
 const [busy, setBusy] = useState(false);
 const dialog = useRef<HTMLDialogElement>(null);
 const search = useRef<HTMLInputElement>(null);
 const paletteTrigger = useRef<HTMLButtonElement>(null);
 useEffect(() => {
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem('codeaf-theme', theme); } catch { /* Theme still works without storage. */ }
 }, [theme]);
 useEffect(() => {
  const onKey = (e: KeyboardEvent) => {
   if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); }
  };
  window.addEventListener('keydown', onKey);
  return () => window.removeEventListener('keydown', onKey);
 }, []);
 useEffect(() => {
  if (palette) { dialog.current?.showModal(); search.current?.focus(); }
  else { dialog.current?.close(); setQuery(''); }
 }, [palette]);
 async function health() {
  setBusy(true);
  try { const h = await checkEngine(); setEngine(`${h.status} · v${h.version} · ${h.platform}`); }
  catch (e) { setEngine(String(e instanceof Error ? e.message : e)); }
  finally { setBusy(false); }
 }
 return <div className="app-shell">
  <aside className="sidebar" aria-label="Main navigation">
   <div className="brand"><span className="brand-mark">c</span><strong>CodeAF</strong><span className="badge">early access</span></div>
   <button ref={paletteTrigger} className="search-button" onClick={() => setPalette(true)}><span>Search anything</span><kbd>⌘ / Ctrl K</kbd></button>
   <div className="section-label">YOUR SPACE</div>
   <nav>{pages.map((p, i) => <button key={p} className={`nav-item ${page === p ? 'active' : ''}`} aria-current={page === p ? 'page' : undefined} onClick={() => setPage(p)}><span aria-hidden="true">{['◈', '◷', '▦'][i]}</span>{p}</button>)}</nav>
   <div className="sidebar-bottom"><span className="section-label">APPEARANCE</span><label className="theme-label">Theme<select value={theme} onChange={e => setTheme(e.target.value as Theme)}><option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option></select></label><div className="profile"><span className="avatar">S</span><div><strong>Your workspace</strong><small>Made for focused work</small></div></div></div>
  </aside>
  <main>
   <header className="toolbar"><span>Personal workspace <span className="breadcrumb">/ {page}</span></span><span className="badge">Foundation · 0.1</span></header>
   <div className="page-content">
    {page === 'Workspace' && <><div className="eyebrow">A LITTLE SPACE TO THINK</div><h1>Good work starts<br/>with a clear space.</h1><p className="intro">A quieter home for your code, conversations, and ideas.<br/>Built to feel at home on your desktop.</p><section className="welcome-card"><div className="card-symbol" aria-hidden="true">✳</div><h2>Your next idea lives here.</h2><p>The CodeAF desktop foundation is ready.<br/>Projects and agent sessions are coming next.</p><button className="primary" onClick={() => setPage('Design system')}>Explore the foundation <span aria-hidden="true">↗</span></button></section><div className="footnote"><span className="dot"/> Room to focus. Space to build.</div></>}
    {page === 'Activity' && <><div className="eyebrow">UNDER THE SURFACE</div><h1>A clean start.</h1><p className="intro">No sessions yet. Check that the bundled engine is ready.</p><section className="panel"><h2>Go engine</h2><p role="status">{engine}</p><button className="primary" disabled={busy} onClick={health}>{busy ? 'Checking…' : 'Check engine'}</button></section></>}
    {page === 'Design system' && <><div className="eyebrow">THE CODEAF STANDARD</div><h1>Quiet by design.</h1><p className="intro">Native typography. Thoughtful spacing. One intentional accent.</p><section className="panel"><h2>Color & surface</h2><div className="swatches">{['canvas','surface','accent','text'].map(s => <div key={s}><div className={`swatch ${s}`}/><small>{s}</small></div>)}</div><h2>Typography</h2><p className="type-sample">The system font feels like home.</p><code>SF Pro · system-ui · Segoe UI · sans-serif</code><h2>Interaction</h2><p>Keyboard access, visible focus, reduced motion, and system appearance are part of the foundation.</p><button className="primary" onClick={() => setPalette(true)}>Open command palette</button></section></>}
   </div>
  </main>
  <dialog ref={dialog} className="command-palette" onCancel={() => setPalette(false)} onClose={() => { setPalette(false); paletteTrigger.current?.focus(); }} aria-label="Command palette"><input ref={search} aria-label="Search commands" placeholder="Where would you like to go?" value={query} onChange={e => setQuery(e.target.value)}/><div className="commands">{pages.filter(p => p.toLowerCase().includes(query.toLowerCase())).map(p => <button key={p} onClick={() => {setPage(p);setPalette(false);}}>Go to {p}<span aria-hidden="true">↵</span></button>)}</div><button className="palette-close" onClick={() => setPalette(false)}>Close <kbd>Esc</kbd></button></dialog>
 </div>;
}
export default App;

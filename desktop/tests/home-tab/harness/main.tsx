// Test-only entry: the Home tab alone, on the frame, so the measurements are the chip and not the shell.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import React, { useState } from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import type { MenuEntry } from '../../../src/components/ui';
import { HomeTab, type HomeTabAppearance } from '../../../src/features/places/home/HomeTab';
import './harness.css';

function Row({ variant, children }: { variant: string; children: React.ReactNode }) {
  return <div className="home-tab-harness-cell" data-variant={variant}>{children}</div>;
}

function Harness() {
  const [log, setLog] = useState<string[]>([]);
  const say = (line: string) => setLog(previous => [...previous, line]);
  const menu: MenuEntry[] = [
    { id: 'rename', label: 'Rename', onSelect: () => say('menu:rename') },
    { id: 'close-place', label: 'Close place', onSelect: () => say('menu:close-place') },
  ];
  const tab = (variant: string, props: { name: string; active?: boolean; switcher?: boolean; needsYou?: string; appearance?: HomeTabAppearance; tabIndex?: number; onSelect?: () => void; onOpenSwitcher?: () => void }) => (
    <Row key={variant} variant={variant}>
      <HomeTab id={`home-tab-${variant}`} tint="tide" menu={menu} {...props}/>
    </Row>
  );
  const variants = ['keyboard', 'rest', 'active', 'hover', 'pressed', 'focus', 'switcher', 'alert', 'quiet'];
  return <main className="home-tab-harness" data-testid="home-tab-harness">
    <div role="tablist" aria-label="Home tabs" aria-owns={variants.map(variant => `home-tab-${variant}`).join(' ')}>
      {tab('keyboard', { name: 'codeaf', tabIndex: 0, onSelect: () => say('select:keyboard') })}
      {tab('rest', { name: 'Config parser', tabIndex: -1, onSelect: () => say('select:rest') })}
      {tab('active', { name: 'codeaf', active: true, tabIndex: -1, onSelect: () => say('select:active') })}
      {tab('hover', { name: 'codeaf', appearance: 'hover', tabIndex: -1 })}
      {tab('pressed', { name: 'codeaf', appearance: 'pressed', tabIndex: -1 })}
      {tab('focus', { name: 'codeaf', appearance: 'focus', tabIndex: -1 })}
      {tab('switcher', { name: 'codeaf', switcher: true, tabIndex: -1, onOpenSwitcher: () => say('switcher') })}
      {tab('alert', { name: 'codeaf', switcher: true, needsYou: 'Needs you in Config parser', tabIndex: -1, onOpenSwitcher: () => say('switcher:alert') })}
      {tab('quiet', { name: 'codeaf', needsYou: '   ', tabIndex: -1 })}
    </div>
    <ul aria-label="Callback log">{log.map((line, index) => <li key={`${index}-${line}`}>{line}</li>)}</ul>
  </main>;
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><Harness/></ThemeProvider>
  </React.StrictMode>,
);

// Test-only entry. The Home wire mounts DecidedRows later; this page is the rows alone.
import '../../../src/design/inputModality';
import '../../../src/App.css';
import { useState } from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { DecidedRows, type DecidedItem } from '../../../src/features/decisions/DecidedRows';

const hour = (hours: number) => new Date(Date.parse('2026-10-10T12:00:00Z') - hours * 3_600_000).toISOString();

/** Nine decisions, oldest first, so the section has to sort. The first three ages are the design's. */
const decisions: DecidedItem[] = [
  { id: 'note', title: 'Filed the brand note', detail: 'brand-voice.md · you added', age: '4h', at: hour(4) },
  { id: 'images', title: 'Started Resize hero images', detail: 'From your note in Launch post · $0.04', age: '3h', at: hour(3), why: { by: 'Launch post', because: 'From your note.' } },
  { id: 'strict', title: 'Kept strict mode as the default', detail: 'Decide strict-mode default · from your earlier decision', age: '2h', at: hour(2), why: { by: 'Config parser', because: 'You allowed go test here 6 times. Reversible.', sure: '97%' } },
  { id: 'staging', title: 'Allowed publishing to staging', detail: 'Launch post · you allowed it twice', age: '1h', at: hour(1), why: { by: 'Launch post', because: 'You allowed it twice.', sure: '97%' } },
  { id: 'price', title: 'Led with free for one seat', detail: 'Learned from 3 of your answers', age: '5h', at: hour(5) },
  { id: 'suite', title: 'Held the launch post', detail: 'Launch timing · reversible', age: '6h', at: hour(6) },
  { id: 'hero', title: 'Cropped the hero', detail: 'Launch post · you allowed it once', age: '7h', at: hour(7) },
  { id: 'tone', title: 'Kept the short sentences', detail: 'brand-voice.md · you added', age: '8h', at: hour(8) },
  { id: 'seat', title: 'Dropped the usage line', detail: 'Pricing · you replaced it', age: '9h', at: hour(9) },
];

function Harness() {
  const [log, setLog] = useState<string[]>([]);
  return <main>
    <div data-testid="decided-home" style={{ width: 640 }}>
      <DecidedRows items={decisions} onOpenWhy={item => setLog(lines => [...lines, item.id])}/>
    </div>
    <ul aria-label="Callback log">{log.map((line, index) => <li key={`${line}-${index}`}>{line}</li>)}</ul>
    <div data-testid="decided-empty"><DecidedRows items={[]}/></div>
    <div data-testid="decided-sparse"><DecidedRows items={[{ id: 'bare', title: 'Kept the note', why: {} }]}/></div>
    <div data-testid="decided-custom">
      <DecidedRows items={[decisions[3]]} renderWhy={() => <span>Open the decision</span>}/>
    </div>
    <div data-testid="state-hover"><DecidedRows items={[decisions[3]]} appearance="hover"/></div>
    <div data-testid="state-pressed"><DecidedRows items={[decisions[3]]} appearance="pressed"/></div>
    <div data-testid="state-focus"><DecidedRows items={[decisions[3]]} appearance="focus"/></div>
  </main>;
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <ThemeProvider><Harness /></ThemeProvider>,
);

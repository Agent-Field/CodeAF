import '../../../src/design/inputModality';
import '../../../src/App.css';
import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { PlanCard, type PlanCardStepData } from '../../../src/features/decisions/PlanCard';

const STEPS: PlanCardStepData[] = [
 { kind: 'hold', target: { task: 't1' }, targetLabel: 'Launch post', text: 'Hold Launch post until the v1 suite passes' },
 { kind: 'ask-place', target: { place: 'p1' }, targetLabel: 'Software', text: 'Ask Software to run the v1 suite today' },
 { kind: 'remember', target: { place: 'p1' }, text: 'Remember: run the v1 suite before any launch' },
];

// The log is the harness's stand-in for the conversation: it records what the card asked the engine to do.
function Harness() {
 const mode = new URLSearchParams(location.search).get('mode');
 const [log, setLog] = useState<string[]>([]);
 const note = (line: string) => setLog(l => [...l, line]);
 const steps = mode === 'gone' ? [{ ...STEPS[0], gone: true }, ...STEPS.slice(1)] : STEPS;
 return <>
  <PlanCard steps={steps} onEdit={s => note(`edit:${s.map(x => x.text).join('|')}`)} onGo={() => note('go')} onCancel={() => note('cancel')}/>
  <pre id="log">{log.join('\n')}</pre>
 </>;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness /></ThemeProvider>);

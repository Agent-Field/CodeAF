import '../../../src/design/inputModality';
import '../../../src/App.css';
import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { WhyPopover, type WhyNextTime } from '../../../src/features/decisions/WhyPopover';

// The log stands in for the engine: it records what the card asked for.
function Harness() {
 const bare = new URLSearchParams(location.search).get('mode') === 'bare';
 const [log, setLog] = useState<string[]>([]);
 const [next, setNext] = useState<WhyNextTime | undefined>();
 const note = (line: string) => setLog(l => [...l, line]);
 return <div style={{ padding: 40 }}>
  <div style={{ fontSize: 12 }}>Allowed automatically by Config parser · <WhyPopover
   {...(bare ? {} : { by: 'Config parser', because: 'You allowed go test here 6 times. Reversible.', sure: 0.97 })}
   nextTime={next} onNextTime={c => { setNext(c); note(`next:${c}`); }} onOverturn={() => note('overturn')} onOpenDecision={() => note('open')}/></div>
  <pre id="log">{log.join('\n')}</pre>
 </div>;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness /></ThemeProvider>);

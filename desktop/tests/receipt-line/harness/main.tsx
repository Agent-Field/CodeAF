import '../../../src/design/inputModality';
import '../../../src/App.css';
import './harness.css';
import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button } from '../../../src/components/ui';
import { ThemeProvider, useTheme } from '../../../src/design/ThemeProvider';
import { GroupedReceipt } from '../../../src/features/decisions/GroupedReceipt';
import { ReceiptLine } from '../../../src/features/decisions/ReceiptLine';

const grouped = [
  { questionId: 'q-hold', placeName: 'Config parser', reason: 'you always allow this here', summary: 'held Launch post' },
  { questionId: 'q-ask', placeName: 'Software', reason: 'you asked it to run the suite', summary: 'asked Software' },
  { questionId: 'q-note', placeName: 'Launch post', reason: 'you asked to remember it', summary: 'saved a note' },
];

function Harness() {
  const { setTheme } = useTheme();
  const [log, setLog] = useState<string[]>([]);
  const note = (line: string) => setLog(lines => [...lines, line]);
  const actions = {
    onOpen: (id: string) => note(`open:${id}`),
    onWhy: (id: string) => note(`why:${id}`),
  };
  return <main>
    <Button onClick={() => setTheme('light')}>Light</Button>
    <Button onClick={() => setTheme('dark')}>Dark</Button>
    <span className="ink3-probe" data-testid="ink3">ink</span>
    <span className="ink2-probe" data-testid="ink2">ink</span>
    <span className="danger-probe" data-testid="danger">ink</span>
    <span className="field-probe" data-testid="field"/>
    <span className="field2-probe" data-testid="field2"/>
    <section data-testid="allowed">
      <ReceiptLine questionId="q-allow" placeName="Config parser" reason="you always allow this here" {...actions}/>
    </section>
    <section data-testid="noreason">
      <ReceiptLine questionId="q-bare" placeName="Config parser" {...actions}/>
    </section>
    <section data-testid="blank">
      <ReceiptLine questionId="q-blank" placeName="  " {...actions}/>
    </section>
    <section data-testid="denied">
      <ReceiptLine questionId="q-deny" placeName="Security" reason="this place does not allow it" denied {...actions}/>
    </section>
    <section data-testid="grouped">
      <GroupedReceipt actions={grouped} {...actions}/>
    </section>
    <section data-testid="single">
      <GroupedReceipt actions={[grouped[0]]} {...actions}/>
    </section>
    <pre id="log">{log.join('\n')}</pre>
  </main>;
}

createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness/></ThemeProvider>);

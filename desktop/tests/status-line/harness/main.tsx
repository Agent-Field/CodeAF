import '../../../src/design/inputModality';
import '../../../src/App.css';
import './harness.css';
import React from 'react';
import ReactDOM from 'react-dom/client';
import { Button, HomeTitle } from '../../../src/components/ui';
import { ThemeProvider, useTheme } from '../../../src/design/ThemeProvider';
import { StatusLine, type DecideStatus } from '../../../src/features/decisions/StatusLine';

function Stack({ id, title, status }: { id: string; title: string; status?: DecideStatus | null }) {
  return <section className="status-line-stack" data-testid={id}>
    <HomeTitle>{title}</HomeTitle>
    <StatusLine status={status}/>
  </section>;
}

function Harness() {
  const { setTheme } = useTheme();
  return <main>
    <Button onClick={() => setTheme('light')}>Light</Button>
    <Button onClick={() => setTheme('dark')}>Dark</Button>
    <span className="ink3-probe" data-testid="ink3">ink</span>
    <Stack id="deciding" title="Marketing" status={{ mode: 'deciding', agreedWeek: 41, totalWeek: 44 }}/>
    <Stack id="learning" title="Marketing" status={{ mode: 'learning', learning: { kind: 'permission:shell-read', agreed: 14, of: 20 } }}/>
    <Stack id="always" title="Marketing" status={{ mode: 'always-ask' }}/>
    <Stack id="none" title="Marketing" status={{ mode: 'none' }}/>
    <Stack id="missing" title="Marketing"/>
    <Stack id="deciding-zero" title="Marketing" status={{ mode: 'deciding', agreedWeek: 0, totalWeek: 4 }}/>
    <Stack id="deciding-over" title="Marketing" status={{ mode: 'deciding', agreedWeek: 5, totalWeek: 4 }}/>
    <Stack id="learning-zero" title="Marketing" status={{ mode: 'learning', learning: { agreed: 0, of: 20 } }}/>
    <Stack id="learning-missing" title="Marketing" status={{ mode: 'learning' }}/>
  </main>;
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider><Harness/></ThemeProvider>
  </React.StrictMode>,
);

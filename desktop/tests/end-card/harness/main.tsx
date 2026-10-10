import '../../../src/design/inputModality';
import '../../../src/App.css';
import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { EndCard } from '../../../src/features/nextup/EndCard';

function Harness() {
 const [returned, setReturned] = useState(false);
 const mode = new URLSearchParams(location.search).get('mode');
 return returned ? <p>Returned to Config stack</p> : <EndCard
  answeredCount={mode === 'unknown' ? undefined : mode === 'zero' ? 0 : 5}
  conversations={mode === 'unknown' ? undefined : mode === 'zero' ? [] : mode === 'one' ? [{ tasksRunning: 1 }] : [{ tasksRunning: 1 }, { tasksRunning: 3 }, { tasksRunning: 0 }]}
  originLabel="Config stack" onReturn={() => setReturned(true)} />;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness /></ThemeProvider>);

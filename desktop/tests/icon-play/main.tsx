import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import '../../src/App.css';
import { ThemeProvider } from '../../src/design/ThemeProvider';
import { Icon } from '../../src/components/ui';

// `check` is a play-once name; `plus` is not, so its key must never animate.
function Harness() {
 const [key, setKey] = useState(0);
 return <main><button onClick={() => setKey(k => k + 1)}>bump</button><Icon name="check" play={key}/><Icon name="plus" play={key}/></main>;
}
createRoot(document.getElementById('root')!).render(<ThemeProvider><Harness/></ThemeProvider>);

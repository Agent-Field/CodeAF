import '../../../src/design/inputModality';
import '../../../src/App.css';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { ToastRegion } from '../../../src/components/ui/Toast';
import { OverturnFlow } from '../../../src/features/decisions/OverturnFlow';

// The page posts to the real route shape; the spec intercepts it, so what the card sends is what is asserted.
const dependents = Number(new URLSearchParams(location.search).get('dependents') ?? '2');
const overturn = async (request: { choice?: string }) => {
 const response = await fetch('/api/engine/decisions/d1/overturn', { method: 'POST', body: JSON.stringify(request) });
 if (!response.ok) throw new Error('The engine refused.');
 return { undo: async () => { await fetch('/api/engine/decisions/d1/overturn/undo', { method: 'POST' }); } };
};
createRoot(document.getElementById('root')!).render(<ThemeProvider>
 <div style={{ padding: 40 }}><OverturnFlow dependents={dependents} onOverturn={overturn} onCancel={() => {}}/></div>
 <ToastRegion/>
</ThemeProvider>);

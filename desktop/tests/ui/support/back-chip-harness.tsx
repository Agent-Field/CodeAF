import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { createRoot } from 'react-dom/client';
import { BackChip } from '../../../src/features/focus-history/BackChip';
import { Breadcrumb } from '../../../src/features/conversation/Breadcrumb';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** Browser fixtures supply navigation callbacks without inventing engine work. */
export function mountBackChip(theme: string, parentName: string) {
  localStorage.setItem('codeaf-theme', theme);
  document.body.dataset.theme = theme;
  const host = document.createElement('main');
  document.body.replaceChildren(host);
  const log = (value: string) => { host.dataset.action = value; };
  createRoot(host).render(<ThemeProvider>
    <BackChip parentName={parentName} onBack={() => log('back')} onDismiss={() => log('dismiss')} />
    <Breadcrumb
      segments={[{ id: null, label: 'Conversation' }, { id: 'parent', label: 'Config stack' }, { id: 'current', label: 'Update fixtures' }]}
      canBack canForward={false} onBack={() => log('header-back')} onForward={() => {}} onNavigate={() => log('navigate')}
    />
    <button onClick={event => event.stopPropagation()}>Own action</button>
    <input aria-label="Draft" />
  </ThemeProvider>);
}

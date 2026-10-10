import { createElement as h, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { ToastRegion } from '../../../src/components/ui/Toast';
import { WhyPopover, type WhyNextTime } from '../../../src/features/decisions/WhyPopover';
import { OverturnFlow } from '../../../src/features/decisions/OverturnFlow';
import { createDecisionsClient } from '../../../src/features/decisions/client';
import { why } from './decisions';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';


/** Mounts the Why? card beside the Overturn confirmation, wired to the real decisions client so the routes they call are the product's own. */
export function mountDecisionDetail(theme: string, dependents: number) {
  localStorage.setItem('codeaf-theme', theme);
  const decisions = createDecisionsClient();
  const host = document.createElement('main');
  host.style.padding = '40px';
  document.body.replaceChildren(host);
  function Detail() {
    const [next, setNext] = useState<WhyNextTime>();
    const [overturning, setOverturning] = useState(false);
    return h('div', null,
      h('div', { style: { fontSize: 12 } }, 'Allowed automatically by Config parser · ',
        h(WhyPopover, { by: why.by, because: 'You allowed this here 6 times. Reversible.', sure: 0.94, nextTime: next,
          onNextTime: (choice: WhyNextTime) => { setNext(choice); void decisions.setDecide('marketing', { alwaysAsk: choice === 'ask' }); },
          onOverturn: () => setOverturning(true), onOpenDecision: () => { host.dataset.opened = 'd1'; } })),
      overturning && h(OverturnFlow, { dependents, onCancel: () => setOverturning(false), onDone: () => setOverturning(false),
        onOverturn: async request => { await decisions.overturn('d1', request); } }));
  }
  createRoot(host).render(h(ThemeProvider, null, h(Detail), h(ToastRegion)));
}

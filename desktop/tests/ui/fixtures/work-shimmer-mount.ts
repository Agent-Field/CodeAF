import { createElement as h } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '../../../src/design/ThemeProvider';
import { WorkBlockView } from '../../../src/features/conversation/work/WorkBlockView';
import { WorkStepView } from '../../../src/features/conversation/work/WorkStepView';
import type { WorkStep } from '../../../src/features/conversation/types';
import '../../../src/styles/tokens.css';
import '../../../src/styles/ui.css';

/** A supplied clock proves settling without waiting on wall time or inventing engine events. */
export function mountWorkShimmer(theme: string) {
  localStorage.setItem('codeaf-theme', theme);
  const host = document.createElement('main');
  host.className = 'work-specimen';
  document.body.replaceChildren(host);
  const root = createRoot(host);
  const step = (id: string, state: WorkStep['state']): WorkStep => ({
    id, title: `Running ${id} parser tests`, titleSource: 'caption', category: 'test', state,
    tookMs: state === 'done' ? 12000 : undefined,
    calls: [{ id, tool: 'bash', hint: 'Run tests', args: '{}', output: '', state: state === 'done' ? 'done' : 'running', startedAt: 1000 }],
  });
  return (settled: boolean, now: number) => root.render(h(ThemeProvider, null, h(WorkBlockView, {
    block: { kind: 'work', id: 'work', live: !settled, steps: [step('first', settled ? 'done' : 'running'), step('last', settled ? 'done' : 'running'), step('next', 'preparing')], notes: [], summary: { steps: 3, calls: 3 } },
    open: true, onToggle: () => {}, now,
  }), h(WorkStepView, { step: step('stale', 'done'), shimmer: true, open: false, onToggle: () => {}, now })));
}

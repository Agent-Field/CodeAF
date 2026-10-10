import { createRoot } from 'react-dom/client';
import { WebStatus } from '../../../src/features/web/WebStatus';
import type { WebErrorKind } from '../../../src/features/web/model';

/** Mounts the owned status component so every sentence can be tested independently of native classification. */
export function mountWebStatusProbe(kind: WebErrorKind) {
  const node = document.createElement('div');
  node.className = 'web-sheet';
  document.body.append(node);
  const root = createRoot(node);
  const calls: string[] = [];
  root.render(<WebStatus kind={kind} onExternal={() => calls.push('external')} onReload={() => calls.push('reload')}/>);
  return { calls, dispose: () => { root.unmount(); node.remove(); } };
}

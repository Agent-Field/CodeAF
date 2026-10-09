import { placeholderPane } from './Placeholder';
import type { KindDef } from './slots';

/** Placeholder. The same glyph serves an interactive shell and a job log; no terminal or job-output bridge backs it yet. */
export const terminalKind: KindDef = { kind: 'terminal', label: 'Terminal', icon: 'terminal', backed: false, pane: placeholderPane('Terminal', 'terminal'), preview: null };

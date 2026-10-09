import { TerminalPane } from '../../terminal/TerminalPane';
import type { KindDef } from './slots';

/**
 * Terminals and jobs (design 3c). One glyph serves an interactive shell and a job log; the engine's
 * terminal API backs both. The pane lives in features/terminal. The ⌘T field's "New terminal" row and the
 * ⌃` key call `openTerminalTab()` and dispatch the tab it returns.
 */
export const terminalKind: KindDef = { kind: 'terminal', label: 'Terminal', icon: 'terminal', backed: true, pane: TerminalPane, preview: null };

export { openTerminalTab, type OpenTerminal } from '../../terminal/open';
export { newTerminalShortcut } from '../../terminal/keys';

import { createElement } from 'react';
import { JobPane } from '../../jobs/JobPane';
import { TerminalPreview } from '../preview/bodies';
import { TerminalPane } from '../../terminal/TerminalPane';
import { terminalTabMenuItems } from '../../terminal/tabMenu';
import { terminalTabMeta } from '../../terminal/tabMeta';
import type { KindDef, PaneRenderProps } from './slots';

/** A pane that names an engine job draws that job's log; every other terminal pane is the interactive shell and the PTY-backed job. */
const TerminalOrJobPane = (props: PaneRenderProps) => createElement(props.pane.job ? JobPane : TerminalPane, props);

/**
 * Terminals and jobs (design 3c). One glyph serves an interactive shell and a job log; the engine's
 * terminal API backs both. The pane lives in features/terminal. The ⌘T field's "New terminal" row and the
 * ⌃` key call `openTerminalTab()` and dispatch the tab it returns.
 * A finished job's right-click adds the header menu's Open log, Run again and Remove job (C-EDGE-6).
 * A job that exited shows `exit N` after its name (C-EDGE-5). A running job and a shell show nothing (3j).
 */
export const terminalKind: KindDef = {
  kind: 'terminal', label: 'Terminal', icon: 'terminal', backed: true, pane: TerminalOrJobPane, preview: TerminalPreview,
  menuItems: terminalTabMenuItems,
  tabMeta: terminalTabMeta,
};

export { openTerminalTab, type OpenTerminal } from '../../terminal/open';
export { newTerminalShortcut } from '../../terminal/keys';

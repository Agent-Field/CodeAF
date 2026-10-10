// ⌘O / Ctrl O (registry id open-file). The focused field handles the chord itself; this hook is what runs when
// that field is not the one in front. It opens a New tab, or focuses one already open, and asks for a file name.
import { useRef } from 'react';
import { shortcutLayer } from '../../../../design/keyboard';
import { useShortcuts } from '../../../../design/useShortcuts';
import type { TabsApi } from '../../context';
import { armFilePrompt, planOpenFile } from './openFile';

function workspaceOnScreen(): boolean {
  const root = document.querySelector('.tab-workspace');
  return !!root && !root.closest('[hidden]');
}

/**
 * Workspace half of open-file. Mounted once with the tab api. A dialog that is open keeps the chord, and so does
 * a workspace that is not the page on screen (it stays mounted while another page is showing).
 */
export function useNewTabKeys(api: TabsApi) {
  const live = useRef(api);
  live.current = api;
  useShortcuts(shortcutLayer.workspace, shortcut => {
    if (shortcut.id !== 'open-file') return false;
    if (!workspaceOnScreen() || document.querySelector('dialog[open]')) return false;
    const plan = planOpenFile(live.current.state);
    if (plan.kind === 'open') {
      armFilePrompt();
      live.current.dispatch({ type: 'new' });
    } else {
      armFilePrompt(plan.paneId);
      live.current.dispatch({ type: 'select', id: plan.paneId });
    }
    return true;
  });
}

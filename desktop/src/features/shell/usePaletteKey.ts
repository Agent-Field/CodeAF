import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import { requestNewTabField } from './newTabField';

/**
 * ⌘K (the registry's 'palette' id) is the New-tab field now; the four-page command palette is retired. It works on
 * every page, so `enterWorkspace` first brings the workspace back when another page is showing.
 */
export function usePaletteKey(enterWorkspace: () => void) {
  useShortcuts(shortcutLayer.app, shortcut => {
    if (shortcut.id !== 'palette') return false;
    enterWorkspace();
    requestNewTabField();
    return true;
  });
}

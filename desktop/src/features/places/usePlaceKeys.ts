// The Places keys (Interactions "Shortcuts"): each matched chord becomes a command (keys.ts) and this hook says what
// the command does to navigation. They register on the app layer, so a surface that uses the same chord first keeps it.
import { useEffect, useMemo } from 'react';
import { registerShortcuts, shortcutLayer, tabShortcuts } from '../../design/keyboard';
import { placeKeyCommand } from './keys';
import { railOrder, railSections } from './shell/selectors';
import type { PlacesShell } from './shell/PlacesShell';
import { requestWorkspace } from './shell/workspaceBus';

export function usePlaceKeys(shell: PlacesShell, onEnterWorkspace: () => void) {
  const graph = shell.places.graph;
  const order = useMemo(() => (graph ? railOrder(railSections(graph, shell.closed, shell.place)) : []), [graph, shell.closed, shell.place]);
  useEffect(() => registerShortcuts(shortcutLayer.app, shortcut => {
    const command = placeKeyCommand(shortcut, { place: shell.place, order });
    if (!command) return false;
    switch (command.type) {
      case 'go-to-chooser': shell.openChooser({ kind: 'go' }); return true;
      case 'all-places': onEnterWorkspace(); shell.openAllPlaces(); return true;
      case 'home':
        if (shell.place === 'now') { shell.warn(new Error('Now has no Home. Go to a place to see its Home.')); return true; }
        onEnterWorkspace();
        requestWorkspace({ type: 'home-ensure', place: shell.place, title: shell.index?.byId.get(shell.place)?.name ?? 'Home', focus: true });
        return true;
      case 'jump': onEnterWorkspace(); void shell.goTo(command.place).catch(shell.warn); return true;
      case 'close-place':
        if (shell.place === 'now') shell.warn(new Error(`Now is always open. Close its tabs with ${tabShortcuts.close}.`));
        else shell.closePlace(shell.place);
        return true;
      case 'up': void shell.up().catch(shell.warn); return true;
      case 'new-window': void shell.goToInNewWindow('now').catch(shell.warn); return true;
      case 'undo': void shell.undoLast().catch(shell.warn); return true;
    }
  }), [shell, order, onEnterWorkspace]);
}

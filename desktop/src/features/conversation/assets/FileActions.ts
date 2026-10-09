import type { MenuEntry } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import { absolutePath, relativePath } from './paths';
import type { AssetContextValue } from './AssetContext';

export type FileAvailability = 'exists' | 'missing' | 'outside';

export const revealLabel = isMac ? 'Reveal in Finder' : 'Reveal in Files';

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    /* A refused clipboard write has nothing useful to report. */
  }
}

/** Menu for a file chip. Native actions are confined to the workspace, so outside paths only copy. */
export function fileMenu(path: string, assets: AssetContextValue, availability: FileAvailability): MenuEntry[] {
  const full = absolutePath(path, assets.workspace);
  const relative = relativePath(path, assets.workspace);
  const live = availability === 'exists';
  return [
    { id: 'open', label: 'Open in editor', icon: 'external', disabled: !live, onSelect: () => void assets.openPath(full).catch(() => undefined) },
    { id: 'reveal', label: revealLabel, icon: 'folderOpen', disabled: !live, onSelect: () => void assets.revealPath(full).catch(() => undefined) },
    { kind: 'separator', id: 'sep' },
    { id: 'copy', label: 'Copy path', icon: 'copy', onSelect: () => void copy(full) },
    { id: 'copy-rel', label: 'Copy relative path', icon: 'copy', disabled: relative === null, onSelect: () => void copy(relative ?? path) },
  ];
}

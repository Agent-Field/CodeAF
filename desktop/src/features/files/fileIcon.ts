// Which glyph a file tab and its header draw. Pure, so it runs under node --test.
import type { IconName } from '../../components/ui/Icon.tsx';
import { extensionOf, fileKind } from '../conversation/assets/paths.ts';

/** JSON family first: these extensions are also "code" in the shared kind list, and Shell 3j asks for file-json. */
const jsonExt = new Set(['json', 'jsonc', 'json5']);

/**
 * The file tab's type icon (Shell 3j, "by type: file-code-2, file-json, file-text, image"; header in Shell 3e).
 * Code draws file-code-2, JSON draws file-json, writing and anything with no type glyph draw file-text
 * (`file` in the registry, the plain file icon Q34 allows), and pictures draw image.
 */
export function fileIcon(path: string): IconName {
  const ext = extensionOf(path);
  if (jsonExt.has(ext)) return 'fileJson';
  const kind = fileKind(path);
  if (kind === 'image') return 'image';
  if (kind === 'code') return 'fileCode2';
  return 'file';
}

/**
 * The icon for one file pane. The path wins over the title: renaming the tab must not turn a JSON file
 * into a text icon, and a specimen that only has a title still goes through `fileIcon` on that title.
 */
export function filePaneIcon(pane: { file?: { path: string }; path?: string; title: string }): IconName {
  return fileIcon(pane.file?.path || pane.path || pane.title);
}

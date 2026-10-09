// Pure rules for file and diff tabs: how a path becomes a tab and how an open tab is found again.
import type { Pane, Tab } from '../tabs/types.ts';
import type { FileTarget } from '../tabs/view-state.ts';
import type { IconName } from '../../components/ui/Icon.tsx';
import { extensionOf, fileKind } from '../conversation/assets/paths.ts';

export type FileTabKind = 'file' | 'diff';

/** Name and folder of a slash separated path. */
export function splitFilePath(path: string): { name: string; dir: string } {
  const at = path.lastIndexOf('/');
  return at < 0 ? { name: path, dir: '' } : { name: path.slice(at + 1), dir: path.slice(0, at) };
}

/** A diff tab opens on Changes, a file tab on the file itself. */
export const defaultView = (kind: FileTabKind): NonNullable<FileTarget['view']> => (kind === 'diff' ? 'changes' : 'file');

/** The tab a file chip opens. It reads through the session of the pane that held the chip. */
export function fileTab(source: Pick<Pane, 'sessionFile'>, path: string, kind: FileTabKind, id: string): Tab {
  return { id, kind, title: splitFilePath(path).name, titleSource: 'manual', pinned: false, draft: '', sessionFile: source.sessionFile, file: { path, view: defaultView(kind) } };
}

const panes = (tab: Tab): Pane[] => (tab.split ? tab.split.panes : [tab]);

/** The id of a tab or pane that already shows this file in this session, so a second click selects it. */
export function findFileTab(tabs: readonly Tab[], candidate: Pick<Pane, 'kind' | 'sessionFile' | 'file'>): string | undefined {
  for (const tab of tabs) {
    const hit = panes(tab).find(pane => pane.kind === candidate.kind && pane.sessionFile === candidate.sessionFile && pane.file?.path === candidate.file?.path);
    if (hit) return hit.id;
  }
  return undefined;
}

/**
 * The file tab's type icon (Shell 2h, "File: by type: file-code-2, file-json, file-text, image").
 * JSON draws file-json and source code draws file-code-2, both from the pinned Lucide package behind
 * the central Icon; prose and anything unknown draw file-text, and pictures draw image (FF10).
 */
export function fileTypeIcon(name: string): IconName {
  const kind = fileKind(name);
  if (kind === 'image') return 'image';
  if (extensionOf(name) === 'json') return 'fileJson';
  if (kind === 'code') return 'fileCode2';
  return 'file';
}

import { useEffect } from 'react';
import { Button, DropdownMenu, Icon, type MenuEntry } from '../../components/ui';
import { copyPathShortcut, isCopyPathShortcut } from '../../design/keyboard';
import { openPath } from '../../design/native';
import type { Handoff } from './useWorkView';

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    /* A refused clipboard write has nothing useful to report. */
  }
}

type Props = {
  path: string;
  workspace: string;
  handoff: Handoff;
  /** A file that cannot be shown always gets the menu: it is the one way forward. */
  menu?: boolean;
  /** True for the pane that has the focus: only it answers the copy-path key. */
  keys?: boolean;
};

/**
 * The handoff to the person's editor. "Open in editor ↗" appears only when the engine runs on this machine;
 * otherwise "Open in ⌄" lists what can still be done: the editor when local, and Copy path.
 */
export function EditorHandoff({ path, workspace, handoff, menu = false, keys = false }: Props) {
  const copyFull = () => void copy(handoff.abs ?? path);
  useEffect(() => {
    if (!keys) return;
    const onKey = (event: KeyboardEvent) => {
      if (!isCopyPathShortcut(event)) return;
      event.preventDefault();
      copyFull();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [keys, handoff.abs, path]);
  const open = () => { if (handoff.abs) void openPath(handoff.abs, workspace).catch(() => undefined); };
  if (handoff.canOpen && !menu) {
    return <Button className="file-editor" onClick={open}>Open in editor<Icon name="arrowUpRight" size="xs"/></Button>;
  }
  const items: MenuEntry[] = [
    ...(handoff.canOpen ? [{ id: 'open', label: 'Open in editor', icon: 'external' as const, onSelect: open }] : []),
    { id: 'copy', label: 'Copy path', icon: 'copy', shortcut: copyPathShortcut, onSelect: copyFull },
    { id: 'copy-relative', label: 'Copy relative path', icon: 'copy', onSelect: () => void copy(path) },
  ];
  return <DropdownMenu label="Open in" items={items}><Button className="file-editor">Open in<Icon name="chevron" size="xs"/></Button></DropdownMenu>;
}

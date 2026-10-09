import { useEffect } from 'react';
import { Button, DropdownMenu, Icon, type MenuEntry } from '../../components/ui';
import { copyPathShortcut, isCopyPathShortcut } from '../../design/keyboard';
import { openPath } from '../../design/native';
import { openEngineEditor } from '../chat/engine-client';
import { editorsDefaultFirst } from './editorsClient';
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
 * The handoff to an editor on the engine machine. One discovered editor keeps the
 * "Open in editor" button. Several become "Open in", default first, then Copy path.
 * A remote engine, a headless engine, or a list that never arrived does not invent a handler.
 */
export function EditorHandoff({ path, workspace, handoff, menu = false, keys = false }: Props) {
  const editors = editorsDefaultFirst(handoff.editors ?? []);
  const launchable = !!handoff.canLaunch && editors.length > 0;
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
  const openDefault = () => { if (handoff.abs) void openPath(handoff.abs, workspace).catch(() => undefined); };
  const openEditor = (id: string) => {
    if (!handoff.session) return;
    void openEngineEditor(handoff.session, path, id).catch(() => undefined);
  };
  const single = launchable && editors.length === 1 && !menu;
  const legacy = !handoff.listed && handoff.canOpen && !menu;
  if (single || legacy) {
    const onClick = single ? () => openEditor(editors[0].id) : openDefault;
    return <Button className="file-editor" onClick={onClick}>Open in editor<Icon name="arrowUpRight" size="xs"/></Button>;
  }
  const items: MenuEntry[] = [
    ...(launchable ? editors.map(editor => ({ id: editor.id, label: editor.name, detail: editor.default ? 'default' : undefined, onSelect: () => openEditor(editor.id) })) : []),
    ...(launchable ? [{ kind: 'separator' as const, id: 'sep-copy' }] : []),
    { id: 'copy', label: 'Copy path', icon: 'copy', shortcut: copyPathShortcut, onSelect: copyFull },
    { id: 'copy-relative', label: 'Copy relative path', icon: 'copy', onSelect: () => void copy(path) },
  ];
  return <DropdownMenu label="Open in" items={items}><Button className="file-editor">Open in<Icon name="chevron" size="xs"/></Button></DropdownMenu>;
}

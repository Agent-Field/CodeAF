import { formatShortcut, isMac } from '../../design/keyboard';
export function KeyboardShortcut({ command, label }: { command?: string; label?: string }) {
 const platformLabel = label ? formatShortcut(label) : undefined;
 return <kbd className="keyboard-shortcut">{platformLabel ?? `${isMac ? '⌘' : 'Ctrl'} ${command ?? ''}`}</kbd>;
}

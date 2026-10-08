export function KeyboardShortcut({ command, label }: { command?: string; label?: string }) {
 const mac = /Mac/.test(navigator.platform);
 const platformLabel = label?.replace('⌘/Ctrl', mac ? '⌘' : 'Ctrl').replace('⇧', mac ? '⇧' : 'Shift');
 return <kbd className="keyboard-shortcut">{platformLabel ?? `${mac ? '⌘' : 'Ctrl'} ${command ?? ''}`}</kbd>;
}

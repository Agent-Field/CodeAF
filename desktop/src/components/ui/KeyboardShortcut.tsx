export function KeyboardShortcut({ command, label }: { command?: string; label?: string }) {
 const mac = /Mac/.test(navigator.platform);
 return <kbd className="keyboard-shortcut">{label ?? `${mac ? '⌘' : 'Ctrl'} ${command ?? ''}`}</kbd>;
}

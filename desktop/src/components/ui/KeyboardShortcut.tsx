import { isMac, spellShortcut } from '../../design/keyboard';
import './kbd.css';

/** The design Kbd: `box` is the 18px mono key cap, `inline` the quiet right-aligned menu spelling. */
export function KeyboardShortcut({ command, label, variant = 'box' }: { command?: string; label?: string; variant?: 'box' | 'inline' }) {
 const text = label ? spellShortcut(label) : `${isMac ? '⌘' : 'Ctrl'} ${command ?? ''}`;
 return <kbd className="keyboard-shortcut" data-variant={variant}>{text}</kbd>;
}

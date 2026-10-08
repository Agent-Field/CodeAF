import { useEffect, useRef, useState } from 'react';
import { Icon, KeyboardShortcut, Text, TextInput } from './ui';
export function CommandPalette({ open, onClose, commands, onSelect }: { open: boolean; onClose: () => void; commands: readonly string[]; onSelect: (command: string) => void }) {
 const [query, setQuery] = useState('');
 const dialog = useRef<HTMLDialogElement>(null);
 const search = useRef<HTMLInputElement>(null);
 const lastFocused = useRef<HTMLElement | null>(null);
 useEffect(() => {
  if (open && !dialog.current?.open) {
   lastFocused.current = document.activeElement as HTMLElement;
   dialog.current?.showModal(); search.current?.focus();
  } else if (!open) { dialog.current?.close(); setQuery(''); }
 }, [open]);
 const matches = commands.filter(p => p.toLowerCase().includes(query.toLowerCase()));
 return <dialog ref={dialog} className="command-palette" onCancel={onClose} onClose={() => { onClose(); lastFocused.current?.focus(); }} aria-label="Command palette">
  <div className="palette-search"><Icon name="search" animated={false}/><TextInput ref={search} aria-label="Search commands" placeholder="Search your space…" value={query} onChange={e => setQuery(e.target.value)}/><button type="button" className="palette-close" onClick={onClose} aria-label="Close command palette"><KeyboardShortcut label="esc"/></button></div>
  <div className="commands">{matches.map(p => <button type="button" key={p} onClick={() => {onSelect(p);onClose();}}>Go to {p}<Icon name="arrow" size="xs"/></button>)}{!matches.length && <Text className="no-results">No matching commands</Text>}</div>
 </dialog>;
}

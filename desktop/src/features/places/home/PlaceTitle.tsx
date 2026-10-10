import { useEffect, useRef, useState } from 'react';
import { TextInput } from '../../../components/ui';

/** Longer than the shared menu's exit animation (duration-overlay). */
const menuHandoffMs = 1000;

/**
 * The Home title while Rename is chosen (Places 8f, Q-14): an input holding the current name, in the title's own type.
 * ↵ saves a changed name, Esc reverts, and an unchanged name simply closes. An empty name is REFUSED: the field stays
 * open and says so through aria-invalid, because closing would look like a save and keeping the old name silently
 * would look like the keystroke was lost. Typing again clears the refusal.
 */
export function PlaceTitleField({ title, onRename, onCancel }: { title: string; onRename?: (name: string) => void; onCancel?: () => void }) {
  const field = useRef<HTMLInputElement>(null);
  const [refused, setRefused] = useState(false);
  // The menu that started the rename gives focus back to its button once its exit animation ends. For that short window the field
  // takes focus back from a menu button, and only from one: never from something the person chose.
  useEffect(() => {
    const claim = () => { field.current?.focus(); field.current?.select(); };
    const reclaim = (event: FocusEvent) => { if ((event.target as Element).hasAttribute('aria-haspopup')) claim(); };
    document.addEventListener('focusin', reclaim);
    const first = setTimeout(claim, 0);
    const done = setTimeout(() => document.removeEventListener('focusin', reclaim), menuHandoffMs);
    return () => { clearTimeout(first); clearTimeout(done); document.removeEventListener('focusin', reclaim); };
  }, []);
  return <TextInput ref={field} className="places-heading-rename type-home-title" defaultValue={title} aria-label="Place name" aria-invalid={refused || undefined} autoComplete="off" spellCheck={false}
    onChange={() => setRefused(false)}
    onKeyDown={event => {
      if (event.nativeEvent.isComposing) return;
      if (event.key === 'Enter') {
        event.preventDefault();
        const name = event.currentTarget.value.trim();
        if (!name) setRefused(true);
        else if (name !== title) onRename?.(name);
        else onCancel?.();
      } else if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel?.(); }
    }}/>;
}

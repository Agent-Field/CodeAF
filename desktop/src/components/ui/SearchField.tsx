import type { Ref } from 'react';
import { Icon } from './Icon';
import { KeyboardShortcut } from './KeyboardShortcut';
import '../../design/inputModality';
import './search-field.css';

export type SearchFieldProps = {
 label: string;
 placeholder?: string;
 value: string;
 onChange: (value: string) => void;
 shortcut?: string;
 ref?: Ref<HTMLInputElement>;
};

/** The owning page registers its search shortcut and focuses this input through the ref. */
export function SearchField({ label, placeholder, value, onChange, shortcut = 'Mod+F', ref }: SearchFieldProps) {
 const hint = shortcut.replace('Mod', '⌘/Ctrl').replace(/\+/g, ' ');
 return <div className="search-field">
  <Icon name="search" />
  <input className="text-input-search" ref={ref} type="search" aria-label={label} placeholder={placeholder} value={value}
   onChange={event => onChange(event.currentTarget.value)}
   onKeyDown={event => {
    if (event.key !== 'Escape' || event.nativeEvent.isComposing) return;
    // Escape belongs to the field before an enclosing page or dialog handles dismissal.
    event.preventDefault();
    event.stopPropagation();
    if (value) onChange('');
    event.currentTarget.blur();
   }} />
  {!value && shortcut && <span className="search-field-hint" aria-hidden="true"><KeyboardShortcut label={hint} variant="inline" /></span>}
 </div>;
}

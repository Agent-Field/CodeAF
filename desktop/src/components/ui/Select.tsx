import { useEffect, useRef, useState } from 'react';
import * as Primitive from '@radix-ui/react-select';
import { Icon, type IconName } from './Icon';
import design from '../../design/tokens.json';
import './select.css';
export interface SelectOption { value: string; label: string; disabled?: boolean }
export function Select({ label, value, onValueChange, options, className = '', disabled = false, icon }: { label: string; icon?: IconName; value: string; onValueChange: (value: string) => void; options: readonly SelectOption[]; className?: string; disabled?: boolean }) {
 const [open, setOpen] = useState(false);
 const trigger = useRef<HTMLButtonElement>(null);
 // Native modal dialogs create their own top layer; keep the popup in that layer.
 const modal = trigger.current?.closest('dialog') ?? undefined;
 useEffect(() => {
  if (!open) return;
  // A nested modal already isolates the page. Only its sidebar is the popup background.
  const background = trigger.current?.closest('dialog')
   ? trigger.current.closest<HTMLElement>('.sidebar') : document.getElementById('root');
  if (!background) return;
  const previous = background.inert;
  background.inert = true;
  return () => { background.inert = previous; };
 }, [open]);
 return <Primitive.Root disabled={disabled} open={open} onOpenChange={setOpen} value={value} onValueChange={onValueChange}>
  <Primitive.Trigger ref={trigger} className={`select-trigger ${icon ? 'select-trigger-icon ' : ''}${className}`} aria-label={label} title={label}>{icon ? <Icon name={icon} size="sm"/> : <><Primitive.Value/><Primitive.Icon asChild><Icon name="chevron" size="xs" motion="disclosure"/></Primitive.Icon></>}</Primitive.Trigger>
  <Primitive.Portal container={modal}><Primitive.Content aria-label={label} className="select-menu" position="popper" side="top" align="start" sideOffset={design.overlay.sideOffset} collisionPadding={design.overlay.collisionPadding}>
   <Primitive.Viewport>{options.map(option => <Primitive.Item key={option.value} className="select-option" value={option.value} disabled={option.disabled}>
    {/* The 14px column stays on every row so the labels do not jump when the check appears. */}
    <span className="select-check"><Primitive.ItemIndicator><Icon name="check" size="sm"/></Primitive.ItemIndicator></span>
    <Primitive.ItemText>{option.label}</Primitive.ItemText>
   </Primitive.Item>)}</Primitive.Viewport>
  </Primitive.Content></Primitive.Portal>
 </Primitive.Root>;
}

/** Measured on its own page (`?specimen=select`) so the shell's other menus stay out of the way. The product never navigates here. */
export function SelectSpecimen() {
 const [value, setValue] = useState('beta');
 const options: SelectOption[] = [
  { value: 'alpha', label: 'Alpha' },
  { value: 'beta', label: 'Beta' },
  { value: 'gamma', label: 'Gamma', disabled: true },
  { value: 'delta', label: 'Delta' },
 ];
 return <div className="select-specimen"><Select label="Sample choice" value={value} onValueChange={setValue} options={options}/></div>;
}

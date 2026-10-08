import { useEffect, useState } from 'react';
import * as Primitive from '@radix-ui/react-select';
import { Icon } from './Icon';
import design from '../../design/tokens.json';
export interface SelectOption { value: string; label: string; disabled?: boolean }
export function Select({ label, value, onValueChange, options, className = '' }: { label: string; value: string; onValueChange: (value: string) => void; options: readonly SelectOption[]; className?: string }) {
 const [open, setOpen] = useState(false);
 useEffect(() => {
  if (!open) return;
  // The popup is portaled outside root. Make the background truly unfocusable, not just aria-hidden.
  const background = document.getElementById('root');
  if (!background) return;
  const previous = background.inert;
  background.inert = true;
  return () => { background.inert = previous; };
 }, [open]);
 return <Primitive.Root open={open} onOpenChange={setOpen} value={value} onValueChange={onValueChange}>
  <Primitive.Trigger className={`select-trigger ${className}`} aria-label={label}><Primitive.Value/><Primitive.Icon asChild><Icon name="chevron" size="xs" motion="disclosure"/></Primitive.Icon></Primitive.Trigger>
  <Primitive.Portal><Primitive.Content aria-label={label} className="select-menu" position="popper" side="top" align="start" sideOffset={design.overlay.sideOffset} collisionPadding={design.overlay.collisionPadding}>
   <Primitive.Viewport>{options.map(option => <Primitive.Item key={option.value} className="select-option" value={option.value} disabled={option.disabled}>
    <Primitive.ItemText>{option.label}</Primitive.ItemText><Primitive.ItemIndicator><Icon name="check" size="xs"/></Primitive.ItemIndicator>
   </Primitive.Item>)}</Primitive.Viewport>
  </Primitive.Content></Primitive.Portal>
 </Primitive.Root>;
}

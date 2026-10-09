import { useEffect, useId, useRef, useState, type ReactElement } from 'react';
import * as Primitive from '@radix-ui/react-hover-card';
import { Text } from './Typography';
import design from '../../design/tokens.json';

/** Delayed, noninteractive draft preview. It exposes real content, never fabricated activity.
 * A press closes it and keeps it shut until the pointer leaves: focus and the hover timer
 * would otherwise reopen it over the content the press just opened. */
export function HoverPreview({ children, title, description, meta, disabled = false }: { children: ReactElement; title: string; description: string; meta?: string; disabled?: boolean }) {
 const [open, setOpen] = useState(false);
 const trigger = useRef<HTMLAnchorElement>(null);
 const pressed = useRef(false);
 const change = (value: boolean) => { if (!(value && pressed.current)) setOpen(!disabled && value); };
 const press = () => { pressed.current = true; setOpen(false); };
 const id = useId();
 const modal = trigger.current?.closest('dialog') ?? undefined;
 useEffect(() => {
  if (!open || disabled) return;
  const dismiss = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
  window.addEventListener('keydown', dismiss); return () => window.removeEventListener('keydown', dismiss);
 }, [open, disabled]);
 useEffect(() => { if (disabled) setOpen(false); }, [disabled]);
 return <Primitive.Root open={open && !disabled} onOpenChange={change} openDelay={design.interaction.previewOpenDelay} closeDelay={design.interaction.previewCloseDelay}>
  <Primitive.Trigger ref={trigger} asChild aria-describedby={open && !disabled ? id : undefined} onFocusCapture={event => { if (!disabled && !pressed.current && event.currentTarget.matches(':focus-visible')) setOpen(true); }} onBlurCapture={() => setOpen(false)} onContextMenu={() => setOpen(false)} onPointerDown={press} onClick={() => setOpen(false)} onPointerLeave={() => { pressed.current = false; }}>{children}</Primitive.Trigger>
  {!disabled && <Primitive.Portal container={modal}><Primitive.Content id={id} role="tooltip" className="hover-preview" side="bottom" align="start" sideOffset={design.overlay.sideOffset} collisionPadding={design.overlay.collisionPadding}>
   <Text className="hover-preview-title" tone="default">{title}</Text>{description && <Text className="hover-preview-draft">{description}</Text>}{meta && <Text className="hover-preview-meta">{meta}</Text>}
  </Primitive.Content></Primitive.Portal>}
 </Primitive.Root>;
}

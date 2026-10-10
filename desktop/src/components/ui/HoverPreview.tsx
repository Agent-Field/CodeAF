import { useEffect, useId, useRef, useState, type HTMLAttributes, type ReactElement, type ReactNode } from 'react';
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

/**
 * A controlled card anchored under its trigger. The caller owns when it opens (timers, swapping between
 * neighbours, dismissal), so Radix's own open and close are ignored; this only places the card and keeps it
 * clear of the window edges. Unlike HoverPreview it may hold buttons: the pointer can enter it.
 */
export function HoverCard({ open, trigger, triggerProps, children, collisionPadding = design.overlay.collisionPadding, ...content }: {
  open: boolean;
  trigger: ReactElement;
  triggerProps?: HTMLAttributes<HTMLElement>;
  children: ReactNode;
  /** Pixels kept clear of the viewport. The tab preview passes its own 8px; other callers keep the shared overlay padding. */
  collisionPadding?: number;
} & Omit<HTMLAttributes<HTMLDivElement>, 'children'>) {
  return (
    <Primitive.Root open={open} onOpenChange={() => {}}>
      <Primitive.Trigger asChild {...triggerProps}>{trigger}</Primitive.Trigger>
      {open && <Primitive.Portal><Primitive.Content {...content} side="bottom" align="start" sideOffset={design.overlay.sideOffset} collisionPadding={collisionPadding}>{children}</Primitive.Content></Primitive.Portal>}
    </Primitive.Root>
  );
}

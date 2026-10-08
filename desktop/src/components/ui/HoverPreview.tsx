import { useEffect, useId, useRef, useState, type ReactElement } from 'react';
import * as Primitive from '@radix-ui/react-hover-card';
import { Text } from './Typography';
import design from '../../design/tokens.json';

/** Delayed, noninteractive draft preview. It exposes real content, never fabricated activity. */
export function HoverPreview({ children, title, description, meta, disabled = false }: { children: ReactElement; title: string; description: string; meta?: string; disabled?: boolean }) {
 const [open, setOpen] = useState(false);
 const trigger = useRef<HTMLAnchorElement>(null);
 const id = useId();
 const modal = trigger.current?.closest('dialog') ?? undefined;
 useEffect(() => {
  if (!open || disabled) return;
  const dismiss = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
  window.addEventListener('keydown', dismiss); return () => window.removeEventListener('keydown', dismiss);
 }, [open, disabled]);
 useEffect(() => { if (disabled) setOpen(false); }, [disabled]);
 return <Primitive.Root open={open && !disabled} onOpenChange={value => setOpen(!disabled && value)} openDelay={design.interaction.previewOpenDelay} closeDelay={design.interaction.previewCloseDelay}>
  <Primitive.Trigger ref={trigger} asChild aria-describedby={open && !disabled ? id : undefined} onFocusCapture={() => { if (!disabled) setOpen(true); }} onBlurCapture={() => setOpen(false)} onContextMenu={() => setOpen(false)} onPointerDown={() => setOpen(false)}>{children}</Primitive.Trigger>
  {!disabled && <Primitive.Portal container={modal}><Primitive.Content id={id} role="tooltip" className="hover-preview" side="bottom" align="start" sideOffset={design.overlay.sideOffset} collisionPadding={design.overlay.collisionPadding}>
   <Text className="hover-preview-title">{title}</Text><Text className="hover-preview-draft">{description}</Text>{meta && <Text className="hover-preview-meta">{meta}</Text>}
  </Primitive.Content></Primitive.Portal>}
 </Primitive.Root>;
}

import { createContext, useContext, type HTMLAttributes, type ReactNode, type Ref } from 'react';
import { Button, Icon } from '../../../components/ui';
import { kindDef } from '../kinds/registry';
import type { TabKind } from '../kinds/types';
import type { FieldLine } from './content';
import './preview.css';

/** The 6px dot in a card's header: accent while it works, amber when it needs you, red when it failed. */
export type PreviewDot = 'accent' | 'amber' | 'danger';

const bodyOnly = createContext(false);

/** An overview owns its card chrome and reuses the kind's preview content. */
export function PreviewContents({ children }: { children: ReactNode }) {
  return <bodyOnly.Provider value>{children}</bodyOnly.Provider>;
}

type CardProps = Omit<HTMLAttributes<HTMLDivElement>, 'title'> & {
  kind: TabKind;
  /** Header words after the kind icon; defaults to the kind's name. */
  label?: string;
  /** An amber or red dot takes the kind icon's place (a task that needs you, design 3k). */
  lead?: Extract<PreviewDot, 'amber' | 'danger'>;
  /** Right side of the header: state words, counts, "exit 0 · 2m ago". */
  state?: ReactNode;
  title: string;
  /** The one piece that matters: text, a mono field, a screenshot. */
  children?: ReactNode;
  /** The primary action when the tab needs you. */
  actions?: ReactNode;
  ref?: Ref<HTMLDivElement>;
};

/**
 * The preview card (Shell 3k, Components "Tab hover preview · overview card"): 300px, surface, radius 12, sh-2,
 * header (icon, kind, state), title, then one body. The same anatomy draws the overview card.
 */
export function PreviewCard({ kind, label, lead, state, title, children, actions, className = '', ...rest }: CardProps) {
  if (useContext(bodyOnly)) return <>{children}</>;
  return (
    <div {...rest} className={`preview-card ${className}`} data-kind={kind}>
      <div className="preview-head">
        {lead ? <span className="preview-dot" data-tone={lead} aria-hidden="true"/> : <Icon name={kindDef(kind).icon} size="micro"/>}
        <span className="preview-kind">{label ?? kindDef(kind).label}</span>
        {state && <span className="preview-state">{state}</span>}
      </div>
      <span className="preview-title">{title}</span>
      {children}
      {actions && <div className="preview-actions">{actions}</div>}
    </div>
  );
}

/** A state mark and its words in the card header ("● 4 running"). */
export function PreviewState({ dot, children }: { dot?: PreviewDot; children: ReactNode }) {
  return <>{dot && <span className="preview-dot" data-tone={dot} aria-hidden="true"/>}{children}</>;
}

/** Plain body text, at most three lines. */
export function PreviewText({ children }: { children: ReactNode }) {
  return <span className="preview-text">{children}</span>;
}

/** Why an answer did not go through. It sits above the buttons, which stay, so the person can try again or review. */
export function PreviewError({ children }: { children: ReactNode }) {
  return <span className="preview-error" role="alert">{children}</span>;
}

/** The mono field: the last output lines of a terminal, the head of a diff. `--term` ground, radius 8. */
export function PreviewField({ lines, label }: { lines: readonly FieldLine[]; label: string }) {
  if (lines.length === 0) return null;
  return <div className="preview-field" role="group" aria-label={label}>{lines.map((line, index) => <span key={index} className="preview-line" data-tone={line.tone}>{line.text}</span>)}</div>;
}

/** The web card, whole: a real screenshot area on a light page (a page is light whatever the theme), then title and address. */
export function PreviewShot({ title, address, image, heading, excerpt, note }: { title: string; address: string; image?: string; heading?: string; excerpt?: string; /** Said when there is no picture; a sentence about the picture, never drawn as page content. */ note?: string }) {
  const contents = useContext(bodyOnly);
  return (
    <div className={contents ? 'preview-card-shot' : 'preview-card preview-card-shot'} data-kind="web">
      <div className="preview-shot" aria-hidden={image || note ? undefined : true} data-note={!image && note ? true : undefined}>
        {image ? <img src={image} alt="" className="preview-shot-image"/> : <>{heading && <span className="preview-shot-heading">{heading}</span>}{excerpt && <span className="preview-shot-body">{excerpt}</span>}</>}
        {!image && note && <span className="preview-shot-note">{note}</span>}
      </div>
      {!contents && <div className="preview-caption"><span className="preview-title">{title}</span><span className="preview-address">{address}</span></div>}
    </div>
  );
}

/** The card's actions: the primary one is the accent button, the secondary a quiet one. */
export function PreviewButtons({ primary, secondary, busy }: { primary?: { label: string; onClick: () => void }; secondary: { label: string; onClick: () => void }; busy?: boolean }) {
  return <>
    {primary && <Button variant="primary" className="preview-action" disabled={busy} onClick={primary.onClick}>{primary.label}</Button>}
    <Button variant={primary ? 'quiet' : 'primary'} className="preview-action" data-quiet={primary ? true : undefined} disabled={busy} onClick={secondary.onClick}>{secondary.label}</Button>
  </>;
}

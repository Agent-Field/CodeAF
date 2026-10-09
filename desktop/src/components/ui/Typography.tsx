import type { HTMLAttributes } from 'react';
export function PageHeading(props: HTMLAttributes<HTMLHeadingElement>) { return <h1 {...props} className={`type-title ${props.className ?? ''}`}/>; }
/** The place page title (design 8a): its own 28/600 role, never the old PageHeading. */
export function HomeTitle(props: HTMLAttributes<HTMLHeadingElement>) { return <h1 {...props} className={`type-home-title ${props.className ?? ''}`}/>; }
/** The 11px muted label over a page section ("Since yesterday", "Places", "Chats"). */
export function SectionLabel(props: HTMLAttributes<HTMLHeadingElement>) { return <h2 {...props} className={`type-section-label ${props.className ?? ''}`}/>; }
export function SectionHeading(props: HTMLAttributes<HTMLHeadingElement>) { return <h2 {...props} className={`type-section ${props.className ?? ''}`}/>; }
export function Text({ tone = 'muted', ...props }: HTMLAttributes<HTMLParagraphElement> & { tone?: 'muted' | 'default' }) { return <p {...props} className={`type-body text-${tone} ${props.className ?? ''}`}/>; }
export function CodeText(props: HTMLAttributes<HTMLElement>) { return <code {...props} className={`type-code ${props.className ?? ''}`}/>; }

import type { HTMLAttributes } from 'react';
export function PageHeading(props: HTMLAttributes<HTMLHeadingElement>) { return <h1 {...props} className={`type-title ${props.className ?? ''}`}/>; }
export function SectionHeading(props: HTMLAttributes<HTMLHeadingElement>) { return <h2 {...props} className={`type-section ${props.className ?? ''}`}/>; }
export function Text({ tone = 'muted', ...props }: HTMLAttributes<HTMLParagraphElement> & { tone?: 'muted' | 'default' }) { return <p {...props} className={`type-body text-${tone} ${props.className ?? ''}`}/>; }
export function CodeText(props: HTMLAttributes<HTMLElement>) { return <code {...props} className={`type-code ${props.className ?? ''}`}/>; }

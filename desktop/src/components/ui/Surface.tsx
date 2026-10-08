import type { HTMLAttributes } from 'react';
export function Surface({ direction = 'row', ...props }: HTMLAttributes<HTMLElement> & { direction?: 'row' | 'column' }) {
 return <section {...props} className={`settings-section ${direction === 'column' ? 'vertical' : ''} ${props.className ?? ''}`}/>;
}
export function Separator(props: HTMLAttributes<HTMLHRElement>) { return <hr {...props} className={`separator ${props.className ?? ''}`}/>; }

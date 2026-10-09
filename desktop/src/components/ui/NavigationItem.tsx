import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { Icon, type IconName } from './Icon';
interface NavigationItemProps extends ButtonHTMLAttributes<HTMLButtonElement> {
 icon?: IconName;
 active: boolean;
 /** Replaces the icon when the row's identity is not a glyph (a place's tint square). */
 lead?: ReactNode;
 /** Sits at the trailing edge after the label: a status dot, a count or a shortcut. */
 trail?: ReactNode;
}
export function NavigationItem({ icon, active, lead, trail, children, className = '', ...props }: NavigationItemProps) {
 return <button {...props} type="button" className={`nav-item ${active ? 'active' : ''} ${className}`} aria-current={active ? 'page' : undefined}>{lead ?? (icon && <Icon name={icon} size="sm"/>)}<span>{children}</span>{trail}</button>;
}

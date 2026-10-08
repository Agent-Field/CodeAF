import type { ButtonHTMLAttributes } from 'react';
import { Icon, type IconName } from './Icon';
interface NavigationItemProps extends ButtonHTMLAttributes<HTMLButtonElement> { icon: IconName; active: boolean }
export function NavigationItem({ icon, active, children, className = '', ...props }: NavigationItemProps) {
 return <button {...props} type="button" className={`nav-item ${active ? 'active' : ''} ${className}`} aria-current={active ? 'page' : undefined}><Icon name={icon} size="sm"/><span>{children}</span></button>;
}

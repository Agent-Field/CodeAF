import type { ButtonHTMLAttributes, Ref } from 'react';
import { Icon, type IconName, type IconSize } from './Icon';
type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'quiet' | 'secondary' | 'primary'; loading?: boolean };
export function Button({ variant = 'quiet', loading = false, disabled, className = '', type = 'button', ...props }: ButtonProps) {
 return <button {...props} type={type} className={`button button-${variant} ${className}`} disabled={disabled || loading} aria-busy={loading || undefined}/>;
}
type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'aria-label'> & { ref?: Ref<HTMLButtonElement>; label: string; icon: IconName; iconSize?: IconSize };
export function IconButton({ label, icon, iconSize = 'md', className = '', type = 'button', ...props }: IconButtonProps) {
 return <button {...props} type={type} aria-label={label} title={props.title ?? label} className={`icon-button ${className}`}><Icon name={icon} size={iconSize}/></button>;
}

import type { ButtonHTMLAttributes, Ref } from 'react';
import { Icon, type IconName, type IconSize } from './Icon';
import { useTooltip } from './Tooltip';

/** The designer's control set. Primary is the one action per surface; the safe choice is quiet.
 * Raised sits on a surface with sh-1; ghost is the bare, row-like default; danger is soft. */
export type ButtonVariant = 'primary' | 'raised' | 'quiet' | 'ghost' | 'danger';
type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; loading?: boolean };
export function Button({ variant = 'ghost', loading = false, disabled, className = '', type = 'button', ...props }: ButtonProps) {
 return <button {...props} type={type} className={`button button-${variant} ${className}`} disabled={disabled || loading} aria-busy={loading || undefined}/>;
}

/** control: 28px toolbar button. row: 24px action inside a hovered row. message: 26px message action. */
export type IconButtonSize = 'control' | 'row' | 'message';
type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'aria-label'> & { ref?: Ref<HTMLButtonElement>; label: string;  /** Muted shortcut hint after the tooltip text. */ shortcut?: string; icon: IconName; iconSize?: IconSize; size?: IconButtonSize };
/** Icon-only, so it always carries an accessible name and the shared delayed tooltip (title text or the label). */
export function IconButton({ label, icon, iconSize = 'md', size = 'control', className = '', type = 'button', title, shortcut, onPointerEnter, onPointerLeave, onPointerDown, onFocus, onBlur, ...props }: IconButtonProps) {
 const tooltip = useTooltip(title ?? label, { onPointerEnter, onPointerLeave, onPointerDown, onFocus, onBlur }, shortcut);
 return <>
  <button {...props} {...tooltip.props} type={type} aria-label={label} data-size={size} className={`icon-button ${className}`}><Icon name={icon} size={iconSize}/></button>
  {tooltip.element}
 </>;
}

import type { ButtonHTMLAttributes } from 'react';
export function SidebarAction({ variant, active, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant: 'address' | 'favorite' | 'new'; active?: boolean }) {
 const classes = { address: 'address-field', favorite: 'favorite-button', new: 'new-item' };
 return <button {...props} type="button" aria-pressed={variant === 'favorite' ? active : undefined} data-selected={variant === 'favorite' ? active : undefined} className={`${classes[variant]} ${className}`}/>;
}

import type { ButtonHTMLAttributes } from 'react';
export function SidebarAction({ variant, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant: 'address' | 'favorite' | 'new' }) {
 const classes = { address: 'address-field', favorite: 'favorite-button', new: 'new-item' };
 return <button {...props} type="button" className={`${classes[variant]} ${className}`}/>;
}

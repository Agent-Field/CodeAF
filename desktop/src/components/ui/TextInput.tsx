import type { ComponentPropsWithRef } from 'react';
/** bare: an unframed input inside its own surface (palette, composer). field: the designer's 30px field with the focus halo. */
export function TextInput({ appearance = 'bare', ...props }: ComponentPropsWithRef<'input'> & { appearance?: 'bare' | 'field' }) {
 return <input {...props} className={`text-input ${appearance === 'field' ? 'text-input-field' : ''} ${props.className ?? ''}`}/>;
}

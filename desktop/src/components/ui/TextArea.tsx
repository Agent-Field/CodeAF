import type { ComponentPropsWithRef } from 'react';
export function TextArea(props: ComponentPropsWithRef<'textarea'>) {
 return <textarea {...props} className={`text-input text-area ${props.className ?? ''}`}/>;
}

import design from '../../design/tokens.json';
export function BrandMark({ size = 'small' }: { size?: 'small' | 'hero' }) {
 return <svg className={`brand-mark brand-mark-${size}`} viewBox={design.brand.viewBox} aria-hidden="true" focusable="false"><path d={design.brand.path} fill="none" stroke="currentColor" strokeWidth={design.brand.strokeWidth} strokeLinecap="round"/></svg>;
}

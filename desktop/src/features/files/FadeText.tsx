import { useEffect, useRef, useState } from 'react';

/** One line of text that fades out over its last stretch, but only when it is cut (a fade is a mask, never an ellipsis). */
export function FadeText({ className, children }: { className: string; children: string }) {
  const node = useRef<HTMLSpanElement>(null);
  const [cut, setCut] = useState(false);
  useEffect(() => {
    const element = node.current;
    if (!element) return;
    const measure = () => setCut(element.scrollWidth > element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [children]);
  return <span ref={node} className={`file-fade ${className}`} data-cut={cut || undefined}>{children}</span>;
}

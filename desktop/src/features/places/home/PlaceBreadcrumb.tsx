import type { MouseEvent } from 'react';
import { Button, Icon } from '../../../components/ui';
import '../components/places-components.css';

export type Crumb = {
  id: string;
  label: string;
  /** Click: Go to that place, or to All places for the root. */
  onGo: () => void;
  /** Command-click, Control-click, or middle-click: open that place in a new window. */
  onGoInNewWindow?: () => void;
};

function CrumbLink({ crumb, current }: { crumb: Crumb; current: boolean }) {
  // A modifier click opens a window only when that verb exists. Otherwise it is an ordinary Go to,
  // so a trail is never a row of buttons that swallow the click and do nothing.
  const go = (event: MouseEvent<HTMLButtonElement>) => {
    if ((event.metaKey || event.ctrlKey) && crumb.onGoInNewWindow) {
      event.preventDefault();
      crumb.onGoInNewWindow();
    } else crumb.onGo();
  };
  return <Button variant="ghost" className="places-crumb" aria-current={current ? 'page' : undefined} onClick={go}
    onAuxClick={event => { if (event.button === 1 && crumb.onGoInNewWindow) { event.preventDefault(); crumb.onGoInNewWindow(); } }}>{crumb.label}</Button>;
}

/** Ancestors above a place title (Places 8b): "All places › codeaf › Marketing".
 * Every name goes to that place. Command-click, Control-click and middle-click open it in a new window
 * (Interactions, Breadcrumb). The last name is the parent you are inside; it stays a link and carries
 * aria-current so the end of the trail is announced. The title is the place you are in and is not repeated.
 * An empty list draws nothing: the root and Now have no ancestors. */
export function PlaceBreadcrumb({ crumbs }: { crumbs: readonly Crumb[] }) {
  if (crumbs.length === 0) return null;
  const last = crumbs.length - 1;
  return <nav aria-label="Breadcrumb"><ol className="places-crumbs">
    {crumbs.map((crumb, index) => <li key={crumb.id} className="places-crumb-item">
      {index > 0 && <Icon name="chevronRight" size="micro"/>}
      <CrumbLink crumb={crumb} current={index === last}/>
    </li>)}
  </ol></nav>;
}

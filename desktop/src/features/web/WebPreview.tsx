import { useSyncExternalStore } from 'react';
import { nativeWebAvailable } from '../../design/nativeWeb';
import type { PreviewRenderProps } from '../tabs/kinds/slots';
import { addressParts } from './address';
import { shotOf, subscribeShots } from './shots';
import './web.css';

/**
 * A web tab's hover-card and overview body (design 3k): the one kind with a
 * picture, because what a page looks like is its content. The picture is the
 * native view's own snapshot; when there is none the card says why and never
 * draws a stand-in page.
 */
export function WebPreview({ pane, summary }: PreviewRenderProps) {
  const shot = useSyncExternalStore(subscribeShots, () => shotOf(pane.id));
  const url = pane.target?.url ?? '';
  const { site, rest } = addressParts(url);
  const title = summary?.title || pane.title || site;
  const missing = !nativeWebAvailable() ? 'Pages open in the desktop app' : shot === 'unavailable' ? 'No picture of this page here' : 'No picture yet';
  return <div className="web-preview">
    {typeof shot === 'object'
      ? <img className="web-preview-shot" src={shot.image} alt={`Picture of ${title}`}/>
      : <div className="web-preview-shot" data-missing="true"><span>{missing}</span></div>}
    <div className="web-preview-body">
      <span className="web-preview-title">{title}</span>
      {url && <span className="web-preview-url">{site}{rest}</span>}
    </div>
  </div>;
}

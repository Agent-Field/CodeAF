import { useEffect, useRef } from 'react';
import type { WebState } from '../../design/nativeWeb';
import type { PaneActions } from '../tabs/kinds/slots';
import { siteOf } from './address';

/** Native subscriptions belong to views.ts so hidden pages survive tab switches.
 * This seam publishes the mounted pane's page identity through the workspace's
 * engine-ranked summary and persisted target, including navigation with the same title.
 */
export function useWebEvents(state: WebState | null, url: string | undefined, actions: PaneActions) {
  const current = useRef(actions);
  current.current = actions;
  const shown = state?.url || url;
  const title = state?.title.trim() || (shown ? siteOf(shown) : '');

  const failed = !!state?.failure;

  // Workspace callbacks are recreated on render; their identity must not publish another summary.
  useEffect(() => {
    if (title) current.current.onSummary({ title, firstLine: '', digest: shown ?? '', mark: failed ? 'failed' : undefined });
  }, [title, shown, failed]);
  useEffect(() => {
    if (state?.url && state.url !== url && /^https?:/i.test(state.url)) current.current.onView({ target: { url: state.url } });
  }, [state?.url, url]);
}

// Hover-preview host (owned by the hover-preview lane). Called with the tab's select button; returns it wrapped.
import type { ReactElement } from 'react';
import { HoverPreview } from '../../../components/ui';
import { relativeTime } from '../../conversation/tabSummary';
import { focusedPane } from '../model';
import type { TabsApi } from '../context';
import type { Tab } from '../model';

export function withPreview(api: TabsApi, tab: Tab, trigger: ReactElement): ReactElement {
  const pane = focusedPane(tab);
  const summary = api.summaries[pane.id];
  const group = tab.groupId ? api.state.groups.find(g => g.id === tab.groupId)?.title : undefined;
  const meta = [summary?.updatedAt ? relativeTime(summary.updatedAt, api.now) : '', group ?? (tab.pinned ? 'Pinned tab' : '')].filter(Boolean).join(' · ');
  return <HoverPreview disabled={api.overlayOpen} title={tab.title} description={pane.draft.trim() || summary?.digest || ''} meta={meta}>{trigger}</HoverPreview>;
}

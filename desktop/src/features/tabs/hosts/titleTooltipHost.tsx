// Split-segment title tooltip (TA-STRIP-07, design 3l). A plain tab's full-title tooltip lives on the tab
// primitive (`Tab` + `useTitleOverflow`): active and compressed chips, and only when the name is cut.
// A split has no hover preview, so each segment of the active split, and any cut segment, names itself here.
// No tooltip opens while a preview card is open: two cards must not overlap.
import { cloneElement, useState, useSyncExternalStore, type PointerEvent, type FocusEvent, type ReactElement } from 'react';
import { useTooltip } from '../../../components/ui';
import type { TabsApi } from '../context';
import type { Tab } from '../model';

/** True when the title inside this tab button is wider than its box (the strip fades it under a mask, so there is no ellipsis to see). */
const isCut = (button: HTMLElement) => {
  const title = button.querySelector<HTMLElement>('.workspace-tab-title');
  return !!title && title.scrollWidth > title.clientWidth;
};

/** `previewable` is true while this segment's own hover preview can open. A split segment passes false. */
export function useTitleTooltip(api: Pick<TabsApi, 'previews'>, title: string, { active, pinned, previewable }: { active: boolean; pinned: boolean; previewable: boolean }) {
  const [cut, setCut] = useState(false);
  const previewOpen = useSyncExternalStore(api.previews.subscribe, api.previews.get) !== null;
  const measure = (event: PointerEvent<HTMLElement> | FocusEvent<HTMLElement>) => setCut(isCut(event.currentTarget));
  const wanted = (active || pinned || cut) && !previewable && !previewOpen;
  return useTooltip<HTMLElement>(wanted ? title : '', { onPointerEnter: measure, onFocus: measure });
}

type Flags = { active: boolean; pinned: boolean; previewable: boolean };

function TitleTooltip({ api, title, flags, select, compose }: { api: TabsApi; title: string; flags: Flags; select: ReactElement; compose: (trigger: ReactElement) => ReactElement }) {
  const tooltip = useTitleTooltip(api, title, flags);
  return <>{compose(cloneElement(select, tooltip.props))}{tooltip.element}</>;
}

/** A split tab's segment: a split has no hover preview, so every segment of the active split, and any cut one, names itself. */
export const withSegmentTooltip = (api: TabsApi, tab: Tab, title: string, segment: ReactElement): ReactElement => (
  <TitleTooltip api={api} title={title} flags={{ active: tab.id === api.state.activeId, pinned: false, previewable: false }} select={segment} compose={trigger => trigger}/>
);

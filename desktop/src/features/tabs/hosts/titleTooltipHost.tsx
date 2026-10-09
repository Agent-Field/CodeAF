// Title tooltip host (TA-STRIP-07, design 3l): the full title in the shared 500ms tooltip for the tabs whose title
// the strip cannot show or does not show: the active tab, a pinned tab (icon only) and any tab whose title is cut.
// An inactive tab's hover preview already holds its title, so it never gets a tooltip too, and no tooltip opens while
// any preview card is open: two cards must not overlap.
import { cloneElement, useState, useSyncExternalStore, type PointerEvent, type FocusEvent, type ReactElement } from 'react';
import { useTooltip } from '../../../components/ui';
import type { TabsApi } from '../context';
import type { Tab } from '../model';

/** True when the title inside this tab button is wider than its box (the strip fades it under a mask, so there is no ellipsis to see). */
const isCut = (button: HTMLElement) => {
  const title = button.querySelector<HTMLElement>('.workspace-tab-title');
  return !!title && title.scrollWidth > title.clientWidth;
};

/** The hook behind `withTitleTooltip`; `previewable` is true while this tab's own hover preview can open. */
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

/** Wraps a tab's select button. `compose` adds the next host (the hover preview) around the button this host has fitted. */
export function withTitleTooltip(api: TabsApi, tab: Tab, select: ReactElement, compose: (trigger: ReactElement) => ReactElement = trigger => trigger): ReactElement {
  const active = tab.id === api.state.activeId;
  return <TitleTooltip api={api} title={tab.title} flags={{ active, pinned: tab.pinned, previewable: !active && !api.overlayOpen }} select={select} compose={compose}/>;
}

/** A split tab's segment: a split has no hover preview, so every segment of the active split, and any cut one, names itself. */
export const withSegmentTooltip = (api: TabsApi, tab: Tab, title: string, segment: ReactElement): ReactElement => (
  <TitleTooltip api={api} title={title} flags={{ active: tab.id === api.state.activeId, pinned: false, previewable: false }} select={segment} compose={trigger => trigger}/>
);

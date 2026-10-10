import { cloneElement, type HTMLAttributes, type MouseEvent, type ReactElement, type Ref } from 'react';
import { Button, IconButton, type IconName } from '../../components/ui';
import { TabGlyph, type TabState } from './Tab';
import type { TabKind } from './kinds/types';
import './tab.css';
import './split-tab.css';

export type SplitSegment = { id: string; kind: TabKind; title: string; /** A file pane's type icon, from the kind's iconFor. */ icon?: IconName; /** A web pane's site, the monogram source. */ monogram?: string; favicon?: string; state?: TabState };

/**
 * A split is ONE merged tab with a segment per pane (up to four); the focused pane's segment is filled.
 * Each segment is itself a tab-role button so keyboard and screen-reader users reach every pane.
 */
export function SplitTab({ segments, focus, active, picked = false, hover = false, specimen = false, onSelectPane, onClose, wrapSegment, frame, ...rest }: {
  segments: readonly SplitSegment[]; focus: number; active?: boolean; picked?: boolean; hover?: boolean; specimen?: boolean;
  onSelectPane?: (index: number, event: MouseEvent<HTMLButtonElement>) => void; onClose?: () => void;
  /** Wraps each segment's button; the title tooltip host puts the full title here. */
  wrapSegment?: (segment: SplitSegment, button: ReactElement) => ReactElement;
  frame?: HTMLAttributes<HTMLDivElement> & { draggable?: boolean };
} & Omit<HTMLAttributes<HTMLDivElement>, 'onSelect'> & { ref?: Ref<HTMLDivElement> }) {
  const name = segments.map(s => s.title).join(' and ');
  const wrap = (segment: SplitSegment, button: ReactElement) => (wrapSegment ? cloneElement(wrapSegment(segment, button), { key: segment.id }) : button);
  return (
    <div {...rest} {...frame} className={`workspace-tab workspace-split-tab ${frame?.className ?? ''}`} data-active={!!active} data-selected={picked || undefined} data-hover={hover || undefined} role="group" aria-label={`Split: ${name}`}>
      {segments.map((segment, index) => wrap(segment, (
        <Button key={segment.id} className="workspace-split-segment" role={specimen ? undefined : 'tab'} id={`tab-${segment.id}`} aria-controls={specimen ? undefined : 'workspace-tab-panel'} aria-selected={specimen ? undefined : !!active && index === focus} aria-label={segment.title} aria-description={picked ? 'selected for grouping' : undefined} data-focused={!!active && index === focus} tabIndex={active && index === focus ? 0 : -1} onClick={event => onSelectPane?.(index, event)}>
          <TabGlyph icon={segment.icon} favicon={segment.favicon} kind={segment.kind} title={segment.title} monogram={segment.monogram} state={segment.state}/>
          <span className="workspace-tab-title">{segment.title}</span>
        </Button>
      )))}
      {onClose && <span className="workspace-tab-close-slot"><IconButton className="workspace-tab-close" label={`Close split ${name}`} icon="close" iconSize="micro" tabIndex={active ? 0 : -1} onClick={onClose}/></span>}
    </div>
  );
}

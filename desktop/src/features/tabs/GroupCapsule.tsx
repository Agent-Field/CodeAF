import type { HTMLAttributes, ReactElement, ReactNode } from 'react';
import { Button } from '../../components/ui';
import './tab.css';
import './group.css';

export type GroupCapsuleProps = {
  title: string;
  /** Tabs in the group, collapsed or not (the "3" in "Release v2.4 3"). */
  count: number;
  collapsed: boolean;
  /** Amber dot on the collapsed pill when any member needs you. */
  needsYou?: boolean;
  /** Member tabs. When collapsed the caller marks hidden members with `hidden`; see MemberSlot. */
  children: ReactNode;
  onToggle?: () => void;
  /** Drag and drop on the label: the drag host makes it carry the whole group and take dropped tabs and groups. */
  labelProps?: HTMLAttributes<HTMLButtonElement> & { draggable?: boolean };
  /** Wraps the label; the menu lane puts the group context menu here. */
  wrapLabel?: (label: ReactElement) => ReactElement;
};

/**
 * A group is a capsule: a --tab-hover fill, a 12px medium label, then its members. Collapsed it
 * shrinks to the pill "Label N"; the active member stays beside it so the selection never vanishes.
 */
export function GroupCapsule({ title, count, collapsed, needsYou, children, onToggle, labelProps, wrapLabel }: GroupCapsuleProps) {
  const label = (
    <Button {...labelProps} className="workspace-group-label" aria-expanded={!collapsed} onClick={onToggle}>
      {collapsed && needsYou && <span className="tab-dot" data-state="waiting" role="img" aria-label="Needs you"/>}
      <span className="workspace-group-name">{title}</span>
      {collapsed && <span className="workspace-group-count">{count}</span>}
    </Button>
  );
  return <div className="workspace-tab-group" data-collapsed={collapsed}>{wrapLabel ? wrapLabel(label) : label}<div className="workspace-group-tabs">{children}</div></div>;
}

/** One member's wrapper: hidden members collapse to zero width and leave the tab order. */
export function MemberSlot({ hidden, children }: { hidden: boolean; children: ReactNode }) {
  return <div className="workspace-group-tab-slot" data-hidden={hidden} inert={hidden} aria-hidden={hidden ? true : undefined}>{children}</div>;
}

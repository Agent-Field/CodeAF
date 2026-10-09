import type { ReactNode } from 'react';
import { TaskNotice } from './TaskNotice';
import { ThinkingItem } from './ThinkingItem';
import { ToolGroup } from './ToolGroup';
import type { TurnItem } from './types';

export type OpenTask = (taskId: string, background: boolean) => void;

type RendererContext = {
  open: Record<string, boolean>;
  onToggle: (id: string) => void;
  readFull?: (callId: string) => Promise<{ output: string; full: boolean }>;
  onOpenTask: OpenTask;
};

/** Draws the items TurnView leaves to its owner: tools, thinking and task notices. */
export function itemRenderer({ open, onToggle, readFull, onOpenTask }: RendererContext) {
  return function renderItem(item: TurnItem): ReactNode {
    const expanded = Boolean(open[item.id]);
    const toggle = () => onToggle(item.id);
    if (item.kind === 'tools') return <ToolGroup item={item} open={expanded} onToggle={toggle} readFull={readFull} />;
    if (item.kind === 'thinking') return <ThinkingItem item={item} open={expanded} onToggle={toggle} />;
    if (item.kind === 'task') return <TaskNotice item={item} open={expanded} onToggle={toggle} onOpenTask={onOpenTask} />;
    return null;
  };
}

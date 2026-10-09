import type { KeyboardEvent } from 'react';
import { Button, Icon, type IconName } from '../../../components/ui';
import type { Question } from './form';
import { questionKey, type Tab } from './layout';

const ASKER_ICONS: Record<string, IconName> = {
  model: 'thinking',
  engine: 'tool',
  task: 'tasks',
  surface: 'web',
  window: 'terminal',
};

function tabIcon(tab: Tab): IconName {
  if (tab.kind === 'review') return 'checklist';
  if (tab.kind === 'bulk') return 'tool';
  return ASKER_ICONS[tab.question.asker?.kind ?? ''] ?? 'queued';
}

type TabsProps = {
  tabs: Tab[];
  activeId: string;
  answeredIds: Set<string>;
  folded: Question[];
  foldedOpen: boolean;
  onSelect: (id: string) => void;
  onToggleFolded: () => void;
};

export const tabDomId = (id: string) => `tray-tab-${id.replace(/\W+/g, '-')}`;
export const panelDomId = (id: string) => `tray-panel-${id.replace(/\W+/g, '-')}`;

function arrowTarget(event: KeyboardEvent, index: number, count: number): number | null {
  if (event.key === 'ArrowRight') return (index + 1) % count;
  if (event.key === 'ArrowLeft') return (index - 1 + count) % count;
  if (event.key === 'Home') return 0;
  return event.key === 'End' ? count - 1 : null;
}

/** One tab per question, plus the "N waiting" chip for what Later has folded away. */
export function TrayTabs({ tabs, activeId, answeredIds, folded, foldedOpen, onSelect, onToggleFolded }: TabsProps) {
  function onKeyDown(event: KeyboardEvent, index: number) {
    const target = arrowTarget(event, index, tabs.length);
    if (target === null) return;
    event.preventDefault();
    onSelect(tabs[target].id);
    document.getElementById(tabDomId(tabs[target].id))?.focus();
  }
  return (
    <div className="tray-header">
      {tabs.length > 1 && (
        <div className="tray-tabs" role="tablist" aria-label="Waiting on you">
          {tabs.map((tab, index) => (
            <Button
              key={tab.id}
              id={tabDomId(tab.id)}
              role="tab"
              variant={tab.id === activeId ? 'raised' : 'ghost'}
              aria-selected={tab.id === activeId}
              aria-controls={panelDomId(tab.id)}
              tabIndex={tab.id === activeId ? 0 : -1}
              onClick={() => onSelect(tab.id)}
              onKeyDown={(event) => onKeyDown(event, index)}
            >
              <Icon name={tabIcon(tab)} size="xs" />
              <span className="tray-tab-label">{tab.label}</span>
              {tab.kind === 'question' && answeredIds.has(questionKey(tab.question)) && <Icon name="check" size="xs" />}
            </Button>
          ))}
        </div>
      )}
      {folded.length > 0 && (
        <Button className="tray-chip" aria-expanded={foldedOpen} onClick={onToggleFolded}>
          {`${folded.length} waiting`}
        </Button>
      )}
    </div>
  );
}

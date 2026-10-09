import { useState } from 'react';
import { Button, Icon, WorkStateIndicator } from '../../components/ui';
import type { TurnItem } from './types';
import { ToolRow } from './ToolRow';
import { toolLabel } from './tool-family';
import './tools.css';

type Group = Extract<TurnItem, { kind: 'tools' }>;
type ReadFull = (callId: string) => Promise<{ output: string; full: boolean }>;
type Props = { item: Group; open: boolean; onToggle: () => void; readFull?: ReadFull };

function summary(item: Group): { running: boolean; text: string; failed: number } {
  const running = item.steps.find((step) => step.state === 'running');
  const failed = item.steps.filter((step) => step.state === 'failed').length;
  if (running) {
    const hint = running.hint ? ` · ${running.hint}` : '';
    return { running: true, text: `Running ${toolLabel(running.tool)}${hint}`, failed };
  }
  const count = item.steps.length;
  return { running: false, text: `Worked · ${count} ${count === 1 ? 'step' : 'steps'}`, failed };
}

export function ToolGroup({ item, open, onToggle, readFull }: Props) {
  // Row expansion is a local, view-only detail; the group's own state is owned by the caller.
  const [openRows, setOpenRows] = useState<Record<string, boolean>>({});
  if (item.steps.length === 0) return null;
  const { running, text, failed } = summary(item);
  const toggleRow = (id: string) => setOpenRows((rows) => ({ ...rows, [id]: !rows[id] }));
  return (
    <div className="tool-group">
      <Button className="tool-toggle" aria-expanded={open} onClick={onToggle}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        {running && <WorkStateIndicator phase="working" label="Running" />}
        <span className="tool-summary">{text}</span>
        {failed > 0 && <span className="tool-failed-count">{` · ${failed} failed`}</span>}
      </Button>
      {open && (
        <div className="tool-list">
          {item.steps.map((step) => (
            <ToolRow
              key={step.id}
              step={step}
              open={Boolean(openRows[step.id])}
              onToggle={() => toggleRow(step.id)}
              readFull={readFull}
            />
          ))}
        </div>
      )}
    </div>
  );
}

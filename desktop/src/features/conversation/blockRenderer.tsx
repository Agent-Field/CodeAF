// Draws the turn blocks TurnViewV2 leaves to its owner: work, tasks,
// deliverables and question receipts, each from the shared lane components.

import type { ReactNode } from 'react';
import type { EngineTaskRow } from '../chat/engine-client';
import { DeliverableView, FileChip, ImageGrid, LinkChip, type FigureItem } from './assets';
import { NoteItem } from './NoteItem';
import { TaskNotice, type OpenTask } from './TaskNotice';
import { displayCommand } from './tasks/displayCommand';
import { ReceiptLine, type ReceiptState } from './tray/ReceiptLine';
import type { TurnBlock, TurnV2 } from './types';
import { DiffView } from './work/DiffView';
import type { FileStat, ReadFull } from './work/props';
import { argPath } from './work/stats';
import { WorkBlockView } from './work/WorkBlockView';

export type BlockContext = {
  tasks: EngineTaskRow[];
  waiting?: boolean;
  open: Record<string, boolean>;
  onToggle: (id: string, current?: boolean) => void;
  readFull?: ReadFull;
  onOpenTask: OpenTask;
  onFocusQuestion: (questionKey: string) => void;
};

type Block<K extends TurnBlock['kind']> = Extract<TurnBlock, { kind: K }>;

const RECEIPT_STATES: Record<Block<'receipt'>['state'], ReceiptState> = { waiting: 'waiting', decided: 'answered', withdrawn: 'withdrawn' };

// The call row already draws the edit's +N −M beside the chip, so the chip wears only the file.
export const renderFile = (path: string, stat?: FileStat) => <FileChip path={path} source={stat ? 'edit' : 'read'} />;

const renderLink = (url: string) => <LinkChip href={url} />;

function imageOf(block: TurnBlock | undefined): FigureItem | undefined {
  if (block?.kind !== 'deliverable' || block.deliverable.kind !== 'image') return undefined;
  const { path, caption, meta } = block.deliverable;
  return { path, caption, meta };
}

/** Adjacent images share one grid, drawn at the first of them; the rest draw nothing. */
function imageRun(turn: TurnV2, block: TurnBlock): FigureItem[] | null {
  const at = turn.blocks.indexOf(block);
  if (imageOf(turn.blocks[at - 1])) return null;
  const run: FigureItem[] = [];
  for (let next = at; imageOf(turn.blocks[next]); next++) run.push(imageOf(turn.blocks[next])!);
  return run;
}

/** Every edit call in the turn that touched `path`, drawn as its own diff. */
function diffsFor(turn: TurnV2, path: string): ReactNode {
  const calls = turn.blocks.flatMap((b) => (b.kind === 'work' ? b.steps.flatMap((s) => s.calls) : []));
  const edits = calls.filter((call) => call.tool === 'edit' && argPath(call.args) === path);
  return edits.map((call) => <DiffView key={call.id} args={call.args} />);
}

function deliverable(turn: TurnV2, block: Block<'deliverable'>): ReactNode {
  if (block.deliverable.kind !== 'image') {
    return <DeliverableView deliverable={block.deliverable} renderDiff={(path) => diffsFor(turn, path)} />;
  }
  const run = imageRun(turn, block);
  if (!run) return null;
  return run.length > 1 ? <ImageGrid items={run} /> : <DeliverableView deliverable={block.deliverable} />;
}

/** The live command of a running task, cut the way the task log cuts it. */
function liveLine(tasks: EngineTaskRow[], taskId?: string): string | undefined {
  const row = taskId ? tasks.find((candidate) => candidate.ID === taskId) : undefined;
  const command = row?.Status === 'running' ? row.Live?.Command : undefined;
  return command ? displayCommand(command, row?.LiveParts) : undefined;
}

function receipt(block: Block<'receipt'>, ctx: BlockContext): ReactNode {
  const focus = block.state === 'waiting' ? () => ctx.onFocusQuestion(block.questionKey) : undefined;
  return <ReceiptLine state={RECEIPT_STATES[block.state]} text={block.text} onFocus={focus} />;
}

function task(block: Block<'task'>, ctx: BlockContext): ReactNode {
  return (
    <TaskNotice
      item={{ ...block, kind: 'task' }}
      live={liveLine(ctx.tasks, block.taskId)}
      open={Boolean(ctx.open[block.id])}
      onToggle={() => ctx.onToggle(block.id)}
      onOpenTask={ctx.onOpenTask}
    />
  );
}

/** The disclosure follows the whole turn, not each assistant/tool phase. Its stable ordinal survives
 * live-call handoff to canonical entries whose block ids can change while the same work continues. */
export function workDisclosureKey(turn: TurnV2, block: Block<'work'>): string {
  return `${turn.id}:work-disclosure:${turn.blocks.filter(row => row.kind === 'work').indexOf(block)}`;
}

function WorkBlock({ block, turn, ctx }: { block: Block<'work'>; turn: TurnV2; ctx: BlockContext }) {
  const active = turn.state === 'working' || turn.state === 'streaming';
  const key = workDisclosureKey(turn, block);
  const open = ctx.open[key] ?? ctx.open[block.id] ?? active;
  return (
    <WorkBlockView
      block={block}
      paused={active && ctx.waiting}
      open={open}
      onToggle={() => ctx.onToggle(key, open)}
      renderFile={renderFile}
      renderLink={renderLink}
      readFull={ctx.readFull}
    />
  );
}

/** One renderer per turn, since grids and diffs read the turn's other blocks. */
export function blockRenderer(ctx: BlockContext) {
  return (turn: TurnV2) =>
    function renderBlock(block: TurnBlock): ReactNode {
      switch (block.kind) {
        case 'context-note': return <NoteItem text={block.text} undoReceipts={block.undoReceipts}/>;
        case 'work':
          return <WorkBlock block={block} turn={turn} ctx={ctx} />;
        case 'task':
          return task(block, ctx);
        case 'deliverable':
          return deliverable(turn, block);
        case 'receipt':
          return receipt(block, ctx);
        default:
          return null;
      }
    };
}

import type { ReactNode } from 'react';
import type { ToolStep } from '../types';
import { argsRestateHint } from '../tool-family';
import { DiffView } from './DiffView';
import { LinesBlock } from './LinesBlock';
import { TerminalBlock } from './TerminalBlock';
import type { WorkRender } from './props';
import { argString, parseArgs, uncapped } from './stats';
import { splitLines } from './diff';
import { callTarget, targetText } from './target';

const URL_LINE = /^https?:\/\/\S+$/;

/** Search/fetch results as a plain list; the assets lane swaps in link chips through renderLink. */
function LinkList({ output, renderLink }: { output: string; renderLink?: WorkRender['renderLink'] }) {
  const lines = splitLines(output);
  if (lines.length === 0) return null;
  return (
    <ul className="work-list">
      {lines.map((line, at) => (
        <li key={at}>{renderLink && URL_LINE.test(line.trim()) ? renderLink(line.trim()) : line}</li>
      ))}
    </ul>
  );
}

/** Arguments as plain `key: value` lines: a person reads the ask, never the JSON it travelled in. */
function plainArgs(args: string): string {
  return Object.entries(parseArgs(args) ?? {})
    .map(([key, value]) => `${key}: ${typeof value === 'string' ? value : JSON.stringify(value)}`)
    .join('\n');
}

/** The call's input worth showing: nothing when the row's own target already says it. */
function inputText(call: ToolStep): string {
  const said = `${call.hint} ${targetText(callTarget(call))}`;
  return !call.args.trim() || argsRestateHint(call.args, said) ? '' : plainArgs(call.args);
}

function Generic({ call }: { call: ToolStep }) {
  return (
    <>
      <LinesBlock text={inputText(call)} rows={20} label="Input" />
      <LinesBlock text={call.output} rows={20} label="Output" />
    </>
  );
}

function body(call: ToolStep, render: WorkRender): ReactNode {
  if (call.covered) return <p className="work-note">Read together with the call above.</p>;
  const failed = call.state === 'failed' || call.state === 'stopped';
  if (call.tool === 'bash') return <TerminalBlock call={call} readFull={render.readFull} />;
  if (failed) return <LinesBlock text={call.output} rows={20} label="Output" />;
  switch (call.tool) {
    case 'edit':
      return <DiffView args={call.args} />;
    case 'write':
      return <LinesBlock text={uncapped(argString(parseArgs(call.args), 'content')).text} rows={20} label="Content" />;
    case 'read':
      return <LinesBlock text={call.output} rows={30} label="Excerpt" />;
    case 'web_search':
    case 'web_fetch':
      return <LinkList output={call.output} renderLink={render.renderLink} />;
    default:
      return <Generic call={call} />;
  }
}

/** The expanded part of a call row, chosen by tool (ELEMENTS §3.3). */
export function CallDetail({ call, ...render }: { call: ToolStep } & WorkRender) {
  return <div className="work-detail">{body(call, render)}</div>;
}

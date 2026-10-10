// What the two work-line menus put on the clipboard (Interactions I-ICV-9, I-ICV-12). Pure: the
// only I/O is the injected `readFull`, so the text can be proved without a DOM.

import type { ToolStep, WorkBlock, WorkStep } from '../types';
import type { ReadFull } from './props';
import { callTarget, targetText } from './target.ts';

/** The words a call is about: its command for bash, otherwise the path, query or link it names. */
function callLine(call: ToolStep): string {
  const target = callTarget(call);
  return target.kind === 'command' ? `$ ${target.text}` : targetText(target);
}

/** An output the engine left out of the snapshot is fetched; a failed fetch leaves it out of the copy rather than failing the copy. */
async function outputOf(call: ToolStep, readFull?: ReadFull): Promise<string> {
  if (call.output || !call.outputOmitted || !call.callId || !readFull) return call.output;
  return readFull(call.callId).then((result) => result.output, () => '');
}

/** "Copy command or output" on one call: the command for bash, otherwise the output, else what the row says. */
export async function callCopyText(call: ToolStep, readFull?: ReadFull): Promise<string> {
  const target = callTarget(call);
  if (target.kind === 'command') return target.text;
  return (await outputOf(call, readFull)).trim() || targetText(target);
}

/** "Copy command or output" on a step row: every call's, one after another. */
export async function stepCopyText(step: WorkStep, readFull?: ReadFull): Promise<string> {
  const parts = await Promise.all(step.calls.map((call) => callCopyText(call, readFull)));
  return parts.filter(Boolean).join('\n\n');
}

/** One step of the log: its title, then each call's line and output in the order they settled. */
async function stepLog(step: WorkStep, readFull?: ReadFull): Promise<string> {
  const calls = await Promise.all(step.calls.map(async (call) => {
    const output = (await outputOf(call, readFull)).trim();
    return output ? `${callLine(call)}\n${output}` : callLine(call);
  }));
  return [step.title, ...calls].join('\n');
}

/** "Copy log" on the work line: plain text, steps in record order, a blank line between them. */
export async function workLogText(block: WorkBlock, readFull?: ReadFull): Promise<string> {
  const steps = await Promise.all(block.steps.map((step) => stepLog(step, readFull)));
  return steps.join('\n\n');
}

import { useEffect, useRef, useState } from 'react';
import { DropdownMenu, IconButton, type MenuEntry } from '../../components/ui';
import design from '../../design/tokens.json';
import { tabShortcuts } from '../../design/keyboard';
import { finishedHeaderMenu } from './tabMenu';

/** The colour of the 6px glyph before the state words; colour lives only on that glyph. */
export type StateTone = 'running' | 'done' | 'failed' | 'stopped';

export type TerminalHeaderProps = {
  title: string;
  /** "~/codeaf · job": where it runs and what it is. */
  meta: string;
  /** "Running · 2m 14s", "exit 0 · 2m ago"; empty while the state is unknown. */
  words: string;
  tone?: StateTone;
  /** Stop is offered while the process runs. */
  canStop: boolean;
  /** "Remove job" for a job, "Remove terminal" for a shell. */
  removeLabel: string;
  onStop: () => void;
  /** Present for a finished job: starts the same command again in a new tab. */
  onRerun?: () => void;
  /** Only supplied when the engine has a retained log file and a working opener. */
  onOpenLog?: () => void;
  /** A finished job keeps its engine record when its tab is closed. */
  finishedJob?: boolean;
  onClose: () => void;
  /** The text to copy: the selection, else the recent plain output. */
  readOutput: () => Promise<string>;
  onRemove: () => void;
};

/** Design 3c: 48px line, title and place on the left, live state and quiet action buttons on the right. */
export function TerminalHeader({ title, meta, words, tone, canStop, removeLabel, onStop, onRerun, onOpenLog, finishedJob = false, onClose, readOutput, onRemove }: TerminalHeaderProps) {
  const [copied, setCopied] = useState(false);
  const timer = useRef(0);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  async function copy() {
    try { await navigator.clipboard.writeText(await readOutput()); } catch { return; }
    setCopied(true);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), design.interaction.copiedFeedbackMs);
  }
  // Finished jobs share finishedHeaderMenu with the tab right-click, so Open log, Run again and Remove job cannot drift.
  const menu: MenuEntry[] = finishedJob
    ? finishedHeaderMenu({ onOpenLog, onRerun, onRemove, onClose }, tabShortcuts.close)
    : [
      { id: 'copy', label: 'Copy output', icon: 'copy', onSelect: () => void copy() },
      { kind: 'separator', id: 'sep' },
      { id: 'remove', label: removeLabel, danger: true, onSelect: onRemove },
    ];
  return <header className="terminal-header">
    <span className="terminal-identity"><span className="terminal-title">{title}</span>{meta && <span className="terminal-meta">{meta}</span>}</span>
    <span className="terminal-state">{tone && <span className="terminal-mark" data-tone={tone} aria-hidden="true"/>}{words}</span>
    {canStop && <IconButton className="terminal-action" label="Stop" icon="stop" iconSize="sm" onClick={onStop}/>}
    <IconButton className="terminal-action" label={copied ? 'Copied' : 'Copy output'} icon={copied ? 'check' : 'copy'} iconSize="sm" onClick={() => void copy()}/>
    <DropdownMenu label="Terminal actions" className="terminal-menu" items={menu}><IconButton className="terminal-action" label="More" icon="more" iconSize="sm"/></DropdownMenu>
  </header>;
}

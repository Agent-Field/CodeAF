import { useEffect, useImperativeHandle, useRef, type Ref } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { useScrollAdapter, type Scroller } from '../tabs/scroll/useScrollRestore';
import { isTerminalShortcut } from './keys';
import { lineHeightFor, readTerminalTheme, tokenFont, tokenNumber, watchAppearance } from './theme';
import './terminal-screen.css';

/** What a pane may do to the screen. Output goes in as bytes, exactly as the engine sent them. */
export type ScreenHandle = {
  write: (data: Uint8Array | string) => void;
  /** Clears the screen and scrollback (the engine dropped the oldest bytes, or the stream restarts). */
  reset: () => void;
  /** Pushes what comes next to the bottom edge: a job log reads from there, as the design draws it, until it fills the field. */
  pad: () => void;
  selection: () => string;
  size: () => { cols: number; rows: number };
  focus: () => void;
};

type Props = {
  ref?: Ref<ScreenHandle>;
  /** Accessible name of the input and of the output region. */
  label: string;
  /** Keystrokes go to the program. False for a finished terminal or a job log. */
  interactive: boolean;
  /** The block cursor shows while the program can still print. */
  cursor: boolean;
  onData?: (data: string) => void;
  onResize?: (cols: number, rows: number) => void;
  /** Called once the screen is open and sized, with the handle the pane then feeds. */
  onReady?: (handle: ScreenHandle) => void;
};

/**
 * The screen's scroll place for the pane that holds it, read and written through xterm's public buffer API and never by the viewport's DOM offset
 * (which xterm owns). `top` is a buffer line. A saved line is only reachable once the replay of the engine's kept output has grown the buffer to it,
 * so the restore waits for the replay; nothing here writes to the program.
 */
function screenScroller(term: { current: Terminal | null }): Scroller {
  const at = () => { const buffer = term.current?.buffer.active; return { line: buffer?.viewportY ?? 0, base: buffer?.baseY ?? 0 }; };
  return {
    read: () => { const { line, base } = at(); return { top: line, left: 0, end: line >= base }; },
    reachable: spot => !!term.current && (spot.end || at().base >= spot.top),
    write: spot => { if (spot.end) term.current?.scrollToBottom(); else term.current?.scrollToLine(spot.top); },
    settled: spot => (spot.end ? at().line >= at().base : at().line === spot.top),
  };
}

/**
 * One xterm.js screen with the DOM renderer, themed from the interface tokens (ANSI hues at about 60%
 * chroma, Q3) and sized by the fit addon. Everything else about a terminal lives in the pane.
 */
export function TerminalScreen({ ref, label, interactive, cursor, onData, onResize, onReady }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const latest = useRef({ onData, onResize, onReady });
  latest.current = { onData, onResize, onReady };

  const handle = useRef<ScreenHandle>({
    write: data => term.current?.write(data),
    reset: () => term.current?.reset(),
    pad: () => { const t = term.current; if (t) t.write('\n'.repeat(Math.max(0, t.rows - 1))); },
    selection: () => term.current?.getSelection() ?? '',
    size: () => ({ cols: term.current?.cols ?? 0, rows: term.current?.rows ?? 0 }),
    focus: () => term.current?.focus(),
  });
  useImperativeHandle(ref, () => handle.current, []);
  const scrolled = useScrollAdapter('terminal', screenScroller(term));
  const onScrolled = useRef(scrolled);
  onScrolled.current = scrolled;

  useEffect(() => {
    const element = host.current;
    if (!element) return;
    const fontFamily = tokenFont(element, 'font-mono');
    const fontSize = tokenNumber(element, 'terminal-font-size');
    const t = new Terminal({
      theme: readTerminalTheme(element), fontFamily, fontSize,
      lineHeight: lineHeightFor(element, fontFamily, fontSize, tokenNumber(element, 'terminal-leading')),
      cursorStyle: 'block', cursorBlink: false, cursorInactiveStyle: 'block',
      scrollback: 5000, allowProposedApi: false, convertEol: false,
    });
    const fit = new FitAddon();
    t.loadAddon(fit);
    t.open(element);
    t.textarea?.setAttribute('aria-label', label);
    // The workspace's terminal-context chords (see terminalShortcutOf) are not for the shell; every other key is.
    t.attachCustomKeyEventHandler(event => !isTerminalShortcut(event));
    t.onData(data => latest.current.onData?.(data));
    t.onResize(({ cols, rows }) => latest.current.onResize?.(cols, rows));
    t.onScroll(() => onScrolled.current());
    term.current = t;
    fit.fit();
    let frame = 0;
    const observer = new ResizeObserver(() => { cancelAnimationFrame(frame); frame = requestAnimationFrame(() => fit.fit()); });
    observer.observe(element);
    const stopWatching = watchAppearance(() => { t.options.theme = readTerminalTheme(element); });
    latest.current.onReady?.(handle.current);
    return () => { cancelAnimationFrame(frame); observer.disconnect(); stopWatching(); t.dispose(); term.current = null; };
  }, []);

  useEffect(() => { if (term.current) term.current.options.disableStdin = !interactive; }, [interactive]);
  useEffect(() => { term.current?.write(cursor ? '\x1b[?25h' : '\x1b[?25l'); }, [cursor]);
  useEffect(() => { term.current?.textarea?.setAttribute('aria-label', label); }, [label]);

  return <div ref={host} className="terminal-host" role="group" aria-label={`${label} output`}/>;
}

import { useCallback } from 'react';
import { SectionHeading, Surface, Text } from '../../components/ui';
import { AskField } from './AskField';
import { TerminalHeader, type TerminalHeaderProps } from './TerminalHeader';
import { TerminalScreen, type ScreenHandle } from './TerminalScreen';
import './terminal.css';
import './terminal-specimen.css';

// A fixture, only ever drawn on this page: real xterm.js, real header and Ask pill, no engine behind them.
const esc = (code: string, text: string) => `\x1b[${code}m${text}\x1b[0m`;
const running = [
  'goos: darwin  goarch: arm64', 'pkg: codeaf/internal/parse',
  'BenchmarkLexSmall-10        1843209       651.2 ns/op', 'BenchmarkLexLarge-10          14820     80211 ns/op',
  `${esc('32', 'ok')}   codeaf/internal/parse   4.212s`, 'pkg: codeaf/internal/load',
  'BenchmarkLoadEnv-10          212004      5530 ns/op', 'BenchmarkLoadAll-10',
].join('\r\n');
const failed = [
  `$ make test`, `${esc('1;34', '==>')} running 4 packages`,
  `${esc('32', 'ok')}    codeaf/internal/parse   0.412s`, `${esc('33', 'warn')}  codeaf/internal/load    slow: 3.1s`,
  `${esc('31', 'FAIL')}  codeaf/internal/render  --- FAIL: TestWrap (0.00s)`, `        render_test.go:88: want "a b", got "a  b"`,
  esc('36', 'see render_test.go:88') + '  ' + esc('35', 'diff') + '  ' + esc('2', 'dim') + '  ' + esc('4', 'underline'),
  `${esc('1;31', 'make: *** [test] Error 1')}`,
].join('\r\n');

const noop = () => {};
const readNothing = async () => '';

function Card({ header, output, anchorBottom, live }: { header: Omit<TerminalHeaderProps, 'onStop' | 'onRemove' | 'readOutput'>; output: string; anchorBottom: boolean; live: boolean }) {
  const feed = useCallback((screen: ScreenHandle) => { if (anchorBottom) screen.pad(); screen.write(output); }, [anchorBottom, output]);
  return <div className="terminal-specimen-card">
    <div className="terminal-pane">
      <TerminalHeader {...header} onStop={noop} onRemove={noop} readOutput={readNothing}/>
      <div className="terminal-field"><TerminalScreen label={`${header.title} specimen`} interactive={false} cursor={live} onReady={feed}/></div>
      <AskField onAsk={async () => undefined}/>
    </div>
  </div>;
}

/** Design 3c in both themes (the theme switch drives it): a running job, and a terminal that ended with an error. */
export function TerminalSpecimen() {
  return <Surface direction="column">
    <SectionHeading>Terminal and job tabs</SectionHeading>
    <Text>Output on the terminal field in the program's own colours, ANSI hues at about 60% chroma and only here. State in the header; Ask codeaf about this output starts a new conversation.</Text>
    <div className="terminal-specimens">
      <Card header={{ title: 'nightly-bench', meta: '~/codeaf · job', words: 'Running · 2m 14s', tone: 'running', canStop: true, removeLabel: 'Remove job' }} output={running} anchorBottom live/>
      <Card header={{ title: 'zsh', meta: '~/codeaf · terminal', words: 'exit 2 · 3m 5s ago', tone: 'failed', canStop: false, removeLabel: 'Remove terminal' }} output={failed} anchorBottom={false} live={false}/>
    </div>
  </Surface>;
}

import { useState } from 'react';
import { SectionHeading, Surface, Text } from '../../../components/ui';
import type { EngineFileDiff, EngineTextFile } from '../../chat/engine-client';
import type { FileView } from '../FileHeader';
import { FileSurface } from '../FileSurface';
import type { Handoff, Load } from '../useWorkView';
import './files-specimen.css';

const ready = <T,>(value: T): Load<T> => ({ status: 'ready', value });
const path = 'internal/parse/lexer.go';
const identity = { path, name: 'lexer.go', dir: 'internal/parse', abs: `/work/${path}` };
const row = (kind: 'context' | 'add' | 'del', old: number | undefined, next: number | undefined, text: string) => ({ kind, old, new: next, text });

// Design 3e, line for line. Specimen only: none of this is read from an engine.
const diff: EngineFileDiff = {
  ...identity, git: true, base: { kind: 'start', sha: 'abc1234' }, status: 'modified', added: 12, deleted: 3, lines: 140,
  hunks: [
    { header: '@@ 84,10 +84,16 @@ func (l *Lexer) next() Token', oldStart: 84, oldLines: 10, newStart: 84, newLines: 16, section: 'func (l *Lexer) next() Token', lines: [
      row('context', 84, 84, '  switch r := l.peek(); r {'), row('context', 85, 85, "  case '{', '[':"), row('context', 86, 86, '      return l.open(r)'), row('context', 87, 87, "  case ',':"),
      row('del', 88, undefined, '      return Token{Kind: Comma}'),
      row('add', undefined, 88, '      if l.peekClose() && !l.strict {'), row('add', undefined, 89, '          l.skip() // tolerate trailing comma'), row('add', undefined, 90, '          return l.next()'), row('add', undefined, 91, '      }'), row('add', undefined, 92, '      return Token{Kind: Comma, Pos: l.pos}'),
      row('context', 89, 93, "  case '}', ']':"), row('context', 90, 94, '      return l.close(r)'),
    ] },
    { header: '@@ 131,3 +135,4 @@ func (l *Lexer) peekClose() bool', oldStart: 131, oldLines: 3, newStart: 135, newLines: 4, lines: [
      row('context', 131, 135, 'func (l *Lexer) peekClose() bool {'), row('del', 132, undefined, "    return l.src[l.i] == ']'"), row('add', undefined, 136, '    r := l.peekNonSpace()'), row('add', undefined, 137, "    return r == ']' || r == '}'"),
    ] },
  ],
};
const body = Array.from({ length: 140 }, (_, index) => (index === 83 ? '  switch r := l.peek(); r {' : `// line ${index + 1}`)).join('\n');
const text: EngineTextFile = { ...identity, size: body.length, language: 'go', lines: 140, text: body };
const tooLarge: EngineTextFile = { ...identity, path: 'testdata/huge.json', name: 'huge.json', dir: 'testdata', size: 3.4 * 1024 * 1024, lines: 0, text: '', refusal: 'too-large' };
const outsideGit: EngineFileDiff = { ...identity, git: false, status: 'clean', added: 0, deleted: 0, lines: 140, hunks: [] };
const local: Handoff = { abs: `/work/${path}`, canOpen: true };
const remote: Handoff = { abs: `/srv/work/${path}`, canOpen: false };
const noop = () => undefined;

/** One specimen with its own view state, so the toggle works on the Design system page. */
function Case({ label, note, ...props }: { label: string; note: string } & Partial<Parameters<typeof FileSurface>[0]>) {
  const [view, setView] = useState<FileView>(props.view ?? 'changes');
  return <div className="files-specimen-case">
    <Text className="files-specimen-label"><strong>{label}</strong> {note}</Text>
    <div className="files-specimen-card" data-files-specimen={label}>
      <FileSurface path={path} workspace="/work" diff={ready(diff)} text={ready(text)} handoff={local} onNeedText={noop} {...props} view={view} onView={setView}/>
    </div>
  </div>;
}

/** The file and diff tab in each state the design draws or the designer decided. Specimen only: none of this is live data. */
export function FilesSpecimen() {
  return <Surface direction="column">
    <SectionHeading>File and diff tabs</SectionHeading>
    <Text>Specimen. Changes first, a toggle to the whole file, a handoff to the editor. Hunk header, two line-number columns, folds that open in place.</Text>
    <div className="files-specimen">
      <Case label="Changes" note="engine on this machine: Open in editor ↗" />
      <Case label="File" note="the same file, whole" view="file"/>
      <Case label="Base gone" note="start commit no longer in history" diff={ready({ ...diff, base: { kind: 'head', startGone: true } })}/>
      <Case label="Remote engine" note="Open in ⌄ with Copy path" handoff={remote}/>
      <Case label="Too large" note="refused: one muted line and the Open in menu" view="file" path="testdata/huge.json" diff={ready(outsideGit)} text={ready(tooLarge)}/>
      <Case label="Outside git" note="file view only, no toggle, no counts" view="file" diff={ready(outsideGit)}/>
    </div>
  </Surface>;
}

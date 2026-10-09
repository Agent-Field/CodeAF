import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// The typed client for the file and diff tabs, driven against the mock engine.
const b64 = (text: string) => Buffer.from(text).toString('base64');

test('typed work-view client reads text, finds files, lists changes and diffs', async ({ page }) => {
  const mock = await installMockEngine(page, {
    ...plainReply(),
    files: {
      'internal/parse/lexer.go': { mime: 'text/x-go', dataBase64: b64('package parse\n\nfunc next() {}\n') },
      'logo.bin': { mime: 'application/octet-stream', dataBase64: b64('ab\0cd') },
    },
    diffs: {
      'internal/parse/lexer.go': {
        lines: 120,
        hunks: [{ header: '@@ -84,3 +84,4 @@ func (l *Lexer) next() Token', oldStart: 84, oldLines: 3, newStart: 84, newLines: 4, section: 'func (l *Lexer) next() Token', lines: [
          { kind: 'context', old: 84, new: 84, text: 'case \',\':' },
          { kind: 'del', old: 85, text: 'return Token{Kind: Comma}' },
          { kind: 'add', new: 85, text: 'if l.peekClose() && !l.strict {' },
          { kind: 'add', new: 86, text: '}' },
        ] }],
      },
    },
  });
  await page.goto('/');
  const id = mock.snapshot().id;
  const result = await page.evaluate(async (sid) => {
    const path = '/src/features/chat/engine-client.ts'; const client = await import(path);
    const errors: Record<string, string> = {};
    const attempt = async (key: string, run: () => Promise<unknown>) => { try { await run(); } catch (e) { errors[key] = String((e as Error).message); } };
    const text = await client.readEngineText(sid, 'internal/parse/lexer.go');
    const binary = await client.readEngineText(sid, 'logo.bin');
    const found = await client.findEngineFiles(sid, 'lex');
    const none = await client.findEngineFiles(sid, '');
    const changes = await client.engineChanges(sid);
    const diff = await client.engineFileDiff(sid, 'internal/parse/lexer.go');
    const target = await client.engineEditorTarget(sid, 'internal/parse/lexer.go');
    await attempt('outside', () => client.readEngineText(sid, '/etc/passwd'));
    await attempt('missing', () => client.engineFileDiff(sid, 'nope.go'));
    await attempt('locate', () => client.engineEditorTarget(sid, '../x'));
    return { text, binary, found, none, changes, diff, target, errors };
  }, id);
  expect(result.text).toMatchObject({ name: 'lexer.go', dir: 'internal/parse', language: 'go', lines: 3 });
  expect(result.binary).toMatchObject({ refusal: 'binary', text: '' });
  expect(result.found.files).toEqual([{ path: 'internal/parse/lexer.go', name: 'lexer.go', dir: 'internal/parse' }]);
  expect(result.none.files).toEqual([]);
  expect(result.changes).toMatchObject({ git: true, added: 2, deleted: 1 });
  expect(result.changes.files[0]).toMatchObject({ name: 'lexer.go', dir: 'internal/parse', added: 2, deleted: 1 });
  expect(result.diff).toMatchObject({ added: 2, deleted: 1, lines: 120 });
  expect(result.diff.hunks[0].lines.map((l: { kind: string }) => l.kind)).toEqual(['context', 'del', 'add', 'add']);
  expect(result.target).toMatchObject({ local: true, abs: `${mock.snapshot().workspace}/internal/parse/lexer.go` });
  expect(Object.keys(result.errors).sort()).toEqual(['locate', 'missing', 'outside']);
  expect(mock.calls.some(call => call.path.endsWith('/changes'))).toBe(true);
});

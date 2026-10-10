import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const docs = join(dirname(fileURLToPath(import.meta.url)), '../../../docs');
const questions = readFileSync(join(docs, 'DESIGN-QUESTIONS.md'), 'utf8');
const decisions = readFileSync(join(docs, 'DECISIONS.md'), 'utf8');
const owned = readFileSync(join(docs, 'questions/d5-shell.md'), 'utf8');

const rows: { id: string; cov: string[] }[] = [
  { id: 'SH-OQ1', cov: ['SH-084', 'SH-085'] },
  { id: 'SH-OQ2', cov: ['SH-129', 'SH-130', 'SH-143', 'SH-252'] },
  { id: 'SH-OQ3', cov: ['SH-126', 'SH-127', 'SH-278'] },
  { id: 'SH-OQ4', cov: ['SH-075', 'SH-076'] },
  { id: 'SH-OQ5', cov: ['SH-240', 'SH-241'] },
  { id: 'SH-OQ6', cov: ['SH-147', 'SH-148'] },
  { id: 'SH-OQ7', cov: ['SH-111'] },
  { id: 'SH-OQ8', cov: ['SH-210'] },
  { id: 'SH-OQ9', cov: ['SH-072'] },
  { id: 'SH-OQ10', cov: ['SH-013'] },
  { id: 'SH-OQ11', cov: ['SH-043', 'SH-302', 'SH-310'] },
  { id: 'SH-OQ12', cov: ['SH-035'] },
  { id: 'SH-OQ13', cov: ['SH-190'] },
  { id: 'SH-OQ14', cov: ['SH-028'] },
];

function rowOf(source: string, id: string): string {
  const line = source.split('\n').find(item => item.startsWith(`| ${id} |`));
  assert.ok(line, `${id} is missing`);
  return line;
}

test('every shell coverage question has one row, with the cov ids it governs', () => {
  assert.equal(rows.length, 14);
  for (const row of rows) {
    for (const source of [questions, owned]) {
      const line = rowOf(source, row.id);
      for (const id of row.cov) assert.ok(line.includes(id), `${row.id} does not govern ${id}`);
    }
  }
});

test('the shell rows do not reopen owner, designer, preview, overview, new-tab or rail chords', () => {
  const close = rowOf(questions, 'SH-OQ2');
  assert.match(close, /Close and stop/);
  assert.match(close, /no chord/);
  assert.match(close, /Q30/);
  assert.doesNotMatch(close, /⌥⌘1–3 is Close/);
  const rail = rowOf(questions, 'SH-OQ12');
  assert.match(rail, /beside ⌘S/);
  assert.match(rail, /R2/);
  assert.match(rowOf(questions, 'SH-OQ13'), /OV2/);
  assert.match(rowOf(questions, 'SH-OQ5'), /PLD-01/);
  assert.match(rowOf(questions, 'SH-OQ9'), /SH-072/);
  assert.match(rowOf(questions, 'SH-OQ11'), /PLD-13/);
  assert.match(questions, /⌘\/Ctrl B keeps working beside ⌘S/);
});

test('the dated decision names the task and the fourteen rows', () => {
  assert.match(decisions, /## t-d5-sh-decide-shell-questions \(2026-10-10\)/);
  for (const row of rows) assert.ok(decisions.includes(row.id) || owned.includes(row.id));
  assert.match(decisions, /SH-OQ1–SH-OQ14/);
});

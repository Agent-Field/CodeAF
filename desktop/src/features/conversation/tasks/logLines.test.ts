import test from 'node:test';
import assert from 'node:assert/strict';
import { noteLines } from './logLines.ts';
import type { TaskPage } from './taskTypes.ts';

const page = (extra: Partial<TaskPage> = {}): TaskPage => ({ Row: { ID: '2', Title: 'Update fixtures', Status: 'running' }, ...extra });

test('worker records use the design eyebrow while person and conversation voices remain distinct', () => {
  const notes = noteLines(page({ Notes: [
    { Author: 'worker-1', Body: 'Worker words' },
    { Author: '2.1', Body: 'Sibling words' },
    { Body: 'Unnamed worker words' },
    { Author: 'chat', Body: 'Conversation words' },
    { Person: true, Body: 'Person words' },
  ] }), false);
  assert.deepEqual(notes.slice(0, 3).map(note => note.author), Array(3).fill('Note from worker'));
  assert.equal(notes[3].author, 'Conversation');
  assert.equal(notes[4].person, true);
  assert.equal(notes[4].body, 'Person words');
});

test('no notes or blank records produce no entries; landing-only records retain their result form', () => {
  assert.deepEqual(noteLines(page(), false), []);
  assert.deepEqual(noteLines(page({ Notes: [{ Body: '' }, { Body: ' \n ' }] }), false), []);
  const [note] = noteLines(page({ Notes: [{ Author: '2', Body: 'landed on task/fixtures: 2 files' }] }), false);
  assert.equal(note.author, '');
  assert.equal(note.body, '');
  assert.deepEqual(note.landing, { branch: 'task/fixtures', files: '2 files' });
});

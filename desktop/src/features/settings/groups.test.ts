import test from 'node:test';
import assert from 'node:assert/strict';
import { groupRoles, roleStateLine } from './groups.ts';

const role = (id: string, category?: string, extra: Record<string, unknown> = {}) =>
  ({ id, name: id, controls: '', model: 'm', default: 'm', chosen: false, category, ...extra });
const categories = [{ id: 'conversation', name: 'Conversation and tasks' }, { id: 'places', name: 'Places organization' }];

test('roles sit under their sections in the engine order, and empty sections are not drawn', () => {
  const sections = groupRoles([role('tasks', 'conversation'), role('placefiling', 'places'), role('conversation', 'conversation')], categories);
  assert.deepEqual(sections.map(section => [section.category.name, section.roles.map(row => row.id)]), [
    ['Conversation and tasks', ['tasks', 'conversation']],
    ['Places organization', ['placefiling']],
  ]);
  assert.deepEqual(groupRoles([role('tasks', 'conversation')], categories).length, 1);
});

test('a role in a section the engine did not list is still on the page', () => {
  const sections = groupRoles([role('tasks', 'conversation'), role('mystery', 'future')], categories);
  assert.deepEqual(sections.at(-1)?.roles.map(row => row.id), ['mystery']);
});

test('an engine that sends no sections gets one list', () => {
  assert.deepEqual(groupRoles([role('a'), role('b')], undefined).map(section => section.roles.length), [2]);
  assert.deepEqual(groupRoles([], undefined), []);
});

test('a role nothing calls says so, and a following role names whom it follows until chosen', () => {
  const titles = role('naming', 'naming', { name: 'Chat titles' });
  const recap = role('summaries', 'naming', { inherits: 'naming' });
  assert.equal(roleStateLine(role('placefiling', 'places', { live: false }), []), 'Not in use yet. Your choice is kept for when it is.');
  assert.equal(roleStateLine(recap, [titles, recap]), 'Follows Chat titles until you choose.');
  assert.equal(roleStateLine({ ...recap, chosen: true }, [titles, recap]), '');
  assert.equal(roleStateLine(titles, [titles]), '');
});

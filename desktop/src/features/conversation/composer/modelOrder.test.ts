import test from 'node:test';
import assert from 'node:assert/strict';
import { orderModels } from './modelOrder.ts';

const catalog = ['a/one', 'b/two', 'c/three', 'd/four', 'e/five'].map(id => ({ id, name: id }));
const pinned = [{ id: 'c/three', label: 'Three' }, { id: 'a/one', label: 'One' }, { id: 'x/missing', label: 'Gone' }];
const labelOf = (model: { id: string; name?: string }) => model.name ?? model.id;

test('pinned models lead in the pinned order and a pinned model the catalog lacks is left out', () => {
  const { models, pinnedCount } = orderModels(catalog, pinned, 'a/one', labelOf);
  assert.deepEqual(models.slice(0, 2).map(m => [m.id, m.short]), [['c/three', 'Three'], ['a/one', 'One']]);
  assert.equal(pinnedCount, 2);
  assert.deepEqual(models.slice(2).map(m => m.id), ['b/two', 'd/four', 'e/five']);
});

test('a model in use that is not pinned follows the segments and takes no chord', () => {
  const { models, pinnedCount } = orderModels(catalog, pinned, 'd/four', labelOf);
  assert.equal(pinnedCount, 2);
  assert.equal(models[2].id, 'd/four');
  assert.equal(models[2].short, undefined);
});

test('with no pinned model on offer the model in use stands alone', () => {
  const { models, pinnedCount } = orderModels(catalog, [{ id: 'x/missing', label: 'Gone' }], 'b/two', labelOf);
  assert.equal(pinnedCount, 1);
  assert.equal(models[0].id, 'b/two');
  assert.equal(models.length, 5);
});

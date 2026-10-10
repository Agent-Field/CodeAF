import { test, expect } from '@playwright/test';

test('attention delivery stays quiet in front and routes the exact notification item', async ({ page }) => {
 await page.goto('/');
 const result = await page.evaluate(async () => {
  const { createAttentionNotifications, dispatchAttentionFocus } = await import('/src/lib/native/notify.ts');
  let focused = true;
  let items = [{ id: 'q1', chatId: 'chat', kind: 'question', title: 'Release', head: 'Which branch?' }];
  let count = 3;
  const posted: string[] = [], badges: number[] = [];
  const controller = createAttentionNotifications({
   attention: () => items, rows: () => [{ chatId: 'chat', needsYou: count, archived: false }],
   cursor: () => ({ seq: 9, epoch: 'test' }), lastPlaces: () => undefined,
   subscribe: () => () => undefined,
  }, {
   focused: async () => focused, permission: async () => true,
   post: async pending => { posted.push(...pending.filter(item => !item.silent).map(item => item.id)); },
   badge: async value => { badges.push(value); },
  });
  await controller.update(); focused = false; await controller.update();
  items = [...items, { id: 'q2', chatId: 'chat', kind: 'approval', title: 'Release', head: 'Allow?' }];
  await controller.update(); await controller.update();
  items = []; count = 0; await controller.update();
  let clicked;
  window.addEventListener('codeaf:focus-attention', event => { clicked = event.detail; }, { once: true });
  dispatchAttentionFocus('q2'); controller.stop();
  return { posted, badges, clicked };
 });
 expect(result).toEqual({ posted: ['q2'], badges: [3, 0], clicked: { itemId: 'q2' } });
});

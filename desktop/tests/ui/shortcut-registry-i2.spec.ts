import { test, expect } from '@playwright/test';

// The probe sits above the app's own walker (surface + 1), which would otherwise consume ⌘J and ⌘[ before it sees them.
test('Iteration 2 registry routes Home, composer, chat and focus-history chords', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const path = '/src/design/keyboard.ts';
    const { shortcutOf, registerShortcuts, shortcutLayer } = await import(/* @vite-ignore */ path);
    const host = document.createElement('div');
    host.className = 'home-pane';
    host.innerHTML = '<button>Home</button><div class="composer-dock"><textarea></textarea></div>';
    document.body.append(host);
    const target = host.querySelector('button')!;
    const composer = host.querySelector('textarea')!;
    const ids: string[] = [];
    const unregister = registerShortcuts(shortcutLayer.surface + 2, (shortcut: { id: string }) => { ids.push(shortcut.id); return true; });
    const mac = /Mac/.test(navigator.platform);
    const primary = mac ? { metaKey: true } : { ctrlKey: true };
    const emit = (target: Element, key: string, modifiers = primary) => {
      const event = new KeyboardEvent('keydown', { key, ...modifiers, bubbles: true, cancelable: true });
      target.dispatchEvent(event);
      return event.defaultPrevented;
    };
    const home = emit(target, 'ArrowUp');
    const caret = emit(composer, 'ArrowUp');
    emit(document.body, 'ArrowUp');
    emit(target, 'j');
    if (mac) { emit(target, '['); emit(target, ']'); }
    else { emit(target, 'ArrowLeft', { altKey: true } as typeof primary); emit(target, 'ArrowRight', { altKey: true } as typeof primary); }
    const removed = emit(target, 'i');
    const platformTable = [true, false].map(mac => {
      const primary = mac ? { metaKey: true } : { ctrlKey: true };
      return shortcutOf(new KeyboardEvent('keydown', { key: 'ArrowUp', ...primary }), { mac, home: true }).id;
    });
    unregister();
    host.remove();
    return { ids, home, caret, removed, platformTable };
  });
  expect(result).toEqual({ ids: ['up-level', 'turn-previous', 'next-up', 'back', 'forward'], home: true, caret: false, removed: false, platformTable: ['up-level', 'up-level'] });
});

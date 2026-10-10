import assert from 'node:assert/strict';
import test from 'node:test';

import { COVER_SELECTOR } from '../../features/web/covers.ts';
import {
  forgetWebPane, holdWebOverlay, resetWebOverlayForTests, setWebOverlayTransport, trackWebPane,
  webOverlayDragEnd, webOverlayDragStart, whenWebOverlayIdle,
  type WebOverlayTransport,
} from './webOverlay.ts';

function recording(): { calls: string[]; transport: WebOverlayTransport } {
  const calls: string[] = [];
  const transport: WebOverlayTransport = {
    hideAll: async () => { calls.push('hide'); },
    showAll: async panes => { calls.push(`show:${panes.join(',')}`); },
  };
  return { calls, transport };
}

test('nested holds hide once and show once', async () => {
  resetWebOverlayForTests();
  const { calls, transport } = recording();
  setWebOverlayTransport(transport);
  trackWebPane('a', true);
  trackWebPane('b', true);
  const menu = holdWebOverlay('menu');
  const palette = holdWebOverlay('palette');
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide']);
  menu();
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide']);
  palette();
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide', 'show:a,b']);
});

test('release is idempotent', async () => {
  resetWebOverlayForTests();
  const { calls, transport } = recording();
  setWebOverlayTransport(transport);
  const release = holdWebOverlay('menu');
  await whenWebOverlayIdle();
  release();
  release();
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide', 'show:']);
});

test('a pane closed while held is not re-shown', async () => {
  resetWebOverlayForTests();
  const { calls, transport } = recording();
  setWebOverlayTransport(transport);
  trackWebPane('a', true);
  trackWebPane('b', true);
  const release = holdWebOverlay('overview');
  await whenWebOverlayIdle();
  forgetWebPane('a');
  release();
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide', 'show:b']);
});

test('a tab drag holds once and dragend releases once', async () => {
  resetWebOverlayForTests();
  const { calls, transport } = recording();
  setWebOverlayTransport(transport);
  webOverlayDragStart(['application/codeaf-tab']);
  webOverlayDragStart(['application/codeaf-group']);
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide']);
  webOverlayDragEnd();
  webOverlayDragEnd();
  await whenWebOverlayIdle();
  assert.deepEqual(calls, ['hide', 'show:']);
});

test('menus, previews, palette, quick look, overview and toasts are covers', () => {
  // Menu and popover: role=menu, .app-menu, role=dialog, the Radix popper.
  // Palette, Quick Look and the overview are modal dialogs (dialog[open]).
  // Hover previews are .hover-preview and role=tooltip.
  // Toasts are marked data-native-cover rather than added to this list.
  for (const needle of ['[role="menu"]', '.app-menu', '[role="dialog"]', '[data-radix-popper-content-wrapper]', 'dialog[open]', '.hover-preview', '[role="tooltip"]', '[data-native-cover]']) {
    assert.ok(COVER_SELECTOR.includes(needle), needle);
  }
});

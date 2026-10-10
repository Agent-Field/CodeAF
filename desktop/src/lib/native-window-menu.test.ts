import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const menu = readFileSync(new URL('../../src-tauri/src/menu.rs', import.meta.url), 'utf8');
const windows = readFileSync(new URL('../../src-tauri/src/windows.rs', import.meta.url), 'utf8');
const shell = readFileSync(new URL('../../src-tauri/src/lib.rs', import.meta.url), 'utf8');

test('New Window precedes New Tab and existing tab accelerators stay reserved', () => {
  assert.ok(menu.indexOf('"window-new", "New Window"') < menu.indexOf('"tab-new", "New Tab"'));
  for (const [id, accelerator] of [
    ['window-new', 'Cmd+N'], ['tab-new', 'Cmd+T'], ['tab-close', 'Cmd+W'],
    ['tab-close-stop', 'Alt+Cmd+W'], ['tab-reopen', 'Cmd+Shift+T'],
    ['tab-next', 'Cmd+Shift+BracketRight'], ['tab-previous', 'Cmd+Shift+BracketLeft'],
  ]) {
    const escaped = accelerator.replace(/[+]/g, '\\+');
    assert.match(menu, new RegExp(`"${id}",\\s*"[^"\\n]+",\\s*true,\\s*Some\\("${escaped}"\\)`));
  }
});


test('New Window opens Now in Rust before renderer event routing', () => {
  const start = menu.indexOf('if event.id().as_ref() == "window-new"');
  const end = menu.indexOf('if event.id().as_ref() == "window-close"', start);
  assert.ok(start >= 0 && end > start);
  const handler = menu.slice(start, end);
  assert.match(handler, /place_key: "now"\.into\(\)/);
  assert.match(handler, /crate::windows::open\(&app, &window, request\)/);
  assert.match(handler, /tauri::async_runtime::spawn/);
  assert.match(handler, /return;/);
  assert.ok(end < menu.indexOf('action_of(event.id().as_ref())'));
  assert.match(windows, /trusted\(webview\)\?;\s*open_with\(app, &webview\.window\(\), request, on_page_load\)/);
});

test('Close Window has no accelerator and the platform Window submenu is retained', () => {
  assert.match(menu, /"window-close",\s*"Close Window",\s*true,\s*None::<&str>/);
  assert.doesNotMatch(menu, /Cmd\+Shift\+W/);
  assert.match(menu, /let menu = Menu::default\(app\)\?/);
  assert.doesNotMatch(menu, /Submenu::/);
  assert.match(shell, /#\[cfg\(target_os = "macos"\)\]\s*let builder = builder\.menu\(menu::build\)/);
});

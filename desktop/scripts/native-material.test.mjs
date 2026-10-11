import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { design, generatedFiles } from './design-output.mjs';
const read = path => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('main config enables platform effects and token-owned native controls', () => {
 for (const path of ['src-tauri/tauri.conf.json', 'src-tauri/tauri.macos.conf.json']) {
  const main = JSON.parse(read(path)).app.windows.find(window => window.label === 'main');
  assert.equal(main.transparent, true);
  assert.ok(main.windowEffects.effects.includes('sidebar'));
  assert.equal(main.windowEffects.state, 'followsWindowActiveState');
  assert.equal(main.titleBarStyle, 'Overlay');
  assert.equal(main.hiddenTitle, true);
  assert.deepEqual(main.trafficLightPosition, design.nativeWindow.trafficLightPosition);
  assert.equal(read(path), generatedFiles[path]);
 }
 assert.ok(JSON.parse(read('src-tauri/tauri.conf.json')).app.windows[0].windowEffects.effects.includes('mica'));
});

test('new and reopened windows derive the complete main configuration', () => {
 for (const [path, name] of [['src-tauri/src/windows.rs', 'build<R, F>'], ['src-tauri/src/links.rs', 'reopen_main<R: Runtime>']]) {
  const source = read(path).split(`fn ${name}`)[1].split('WebviewWindowBuilder::from_config(app, &config)')[0];
  assert.match(source, /\.config\(\)[\s\S]*\.find\(\|w\| w.label == "main"\)/);
  assert.match(source, /\.clone(?:d)?\(\)/);
 }
 const placed = read('src-tauri/src/windows.rs').split('fn placed_config(')[1].split('fn build<')[0];
 assert.doesNotMatch(placed, /\.(transparent|window_effects|title_bar_style|hidden_title|traffic_light_position)\s*=/);
});

test('only one material stylesheet is imported', () => {
 assert.equal(existsSync(new URL('../src/styles/frame-material.css', import.meta.url)), false);
 assert.equal((read('src/App.tsx').match(/styles\/material\.css/g) ?? []).length, 1);
 assert.doesNotMatch(read('src/styles/material.css'), /\.app-shell\s*\{[^}]*backdrop-filter:\s*blur/);
});

test('Windows support comes from the shell and leaves unsupported versions solid', () => {
 assert.equal(design.nativeWindow.vibrancyWindowsMinBuild, 22000);
 const native = read('src-tauri/src/windows.rs');
 assert.match(native, /windows_version::OsVersion::current\(\)/);
 assert.match(native, /tokens\["nativeWindow"\]\["vibrancyWindowsMinBuild"\]/);
 assert.match(read('src/design/material.ts'), /windowsDesktop && !nativeAvailable \? 'solid'/);
 assert.match(read('src/design/material.ts'), /context.nativeMaterial === true/);
});

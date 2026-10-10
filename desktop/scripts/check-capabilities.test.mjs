import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';

// The integrator installs this exact contract after the native commands land.
// Keeping the permission list explicit makes every future grant a reviewed change.
const target = {
  identifier: 'default',
  windows: ['main', 'w-*'],
  permissions: [
    'core:default',
    'core:window:allow-start-dragging',
    'core:window:allow-toggle-maximize',
    'core:window:allow-set-theme',
    'core:window:allow-set-title',
    'core:window:allow-set-badge-count',
    'core:webview:allow-set-webview-position',
    'dialog:allow-open',
    'notification:default',
  ],
};

function checkContract(files, config) {
  assert.ok(files.length > 0, 'At least one capability JSON must exist');
  const capabilities = [...files];
  const configured = config.app?.security?.capabilities;
  if (configured !== undefined) {
    assert.ok(Array.isArray(configured), 'Configured capabilities must be an array');
    assert.ok(configured.length > 0, 'Configured capabilities must not disable the contract');
    for (const entry of configured) {
      if (typeof entry === 'string') {
        assert.ok(files.some(file => file.identifier === entry), `Unknown capability: ${entry}`);
      } else {
        // Inline grants are another ACL entry point and must obey the same boundary.
        capabilities.push(entry);
      }
    }
    for (const file of files) {
      assert.ok(configured.some(entry => entry === file.identifier),
        `Capability ${file.identifier} must be enabled by tauri.conf.json`);
    }
  }

  const windows = new Set();
  const permissions = new Set();
  for (const capability of capabilities) {
    assert.ok(capability && typeof capability === 'object', 'Capability must be an object');
    const name = capability.identifier ?? '(inline)';
    assert.ok(!Object.hasOwn(capability, 'remote'), `${name}: remote grants are forbidden`);
    assert.ok(capability.webviews === undefined ||
      (Array.isArray(capability.webviews) && capability.webviews.length === 0),
    `${name}: webview selectors are forbidden, including web-*`);
    assert.ok(Array.isArray(capability.windows) && capability.windows.length > 0,
      `${name}: capability must name app windows`);
    for (const window of capability.windows) {
      assert.ok(target.windows.includes(window), `${name}: forbidden window selector ${window}`);
      windows.add(window);
    }
    assert.ok(Array.isArray(capability.permissions), `${name}: permissions must be an array`);
    for (const permission of capability.permissions) {
      const identifier = typeof permission === 'string' ? permission : permission?.identifier;
      assert.ok(typeof identifier === 'string' && !identifier.startsWith('shell:'),
        `${name}: shell-plugin permissions are forbidden`);
      assert.ok(typeof permission === 'string' && target.permissions.includes(permission),
        `${name}: permission outside exact allow-list: ${identifier}`);
      permissions.add(permission);
    }
  }
  assert.deepEqual([...windows].sort(), [...target.windows].sort(),
    'Capability windows must cover exactly main and w-*');
  assert.deepEqual([...permissions].sort(), [...target.permissions].sort(),
    'Permissions must equal the exact native allow-list');

  const appWindows = config.app?.windows;
  assert.ok(Array.isArray(appWindows) && appWindows.some(window => window.label === 'main'),
    'tauri.conf.json must declare the main app window');
  for (const window of appWindows) {
    assert.ok(window.label === 'main' || /^w-.+$/u.test(window.label ?? ''),
      `tauri.conf.json: forbidden app window ${window.label}`);
  }
}

const fixtureConfig = () => ({ app: { windows: [{ label: 'main' }] } });
const fixtureCapability = () => structuredClone(target);

test('capability JSON and Tauri config enforce the native permission contract', () => {
  const directory = new URL('../src-tauri/capabilities/', import.meta.url);
  const capabilities = readdirSync(directory).filter(name => name.endsWith('.json')).sort()
    .map(name => JSON.parse(readFileSync(new URL(name, directory), 'utf8')));
  const config = JSON.parse(readFileSync(new URL('../src-tauri/tauri.conf.json', import.meta.url), 'utf8'));
  checkContract(capabilities, config);
});

test('integrator target JSON passes with default and explicit capability selection', () => {
  checkContract([fixtureCapability()], fixtureConfig());
  const config = fixtureConfig();
  config.app.security = { capabilities: ['default'] };
  checkContract([fixtureCapability()], config);
});

test('rejects missing multiwindow access and every broader window or webview grant', async t => {
  for (const selector of ['web-*', 'web-1', '*', 'w-1', 'other']) {
    await t.test(`window selector ${selector}`, () => {
      const capability = fixtureCapability();
      capability.windows.push(selector);
      assert.throws(() => checkContract([capability], fixtureConfig()), /forbidden window selector/u);
    });
  }
  const mainOnly = fixtureCapability();
  mainOnly.windows = ['main'];
  assert.throws(() => checkContract([mainOnly], fixtureConfig()), /exactly main and w-\*/u);
  for (const webviews of [['web-*'], ['web-1'], ['*']]) {
    assert.throws(() => checkContract([{ ...fixtureCapability(), webviews }], fixtureConfig()),
      /webview selectors are forbidden/u);
  }
});

test('rejects remote grants, shell permissions, extra scopes and missing permissions', () => {
  for (const remote of [{ urls: ['https://example.com/*'] }, { urls: [] }]) {
    assert.throws(() => checkContract([{ ...fixtureCapability(), remote }], fixtureConfig()),
      /remote grants are forbidden/u);
  }
  for (const permission of ['shell:default', { identifier: 'shell:allow-execute' },
    'core:window:allow-close', { identifier: 'http:default', allow: [{ url: 'http://localhost:*' }] }]) {
    const capability = fixtureCapability();
    capability.permissions.push(permission);
    assert.throws(() => checkContract([capability], fixtureConfig()), /forbidden|outside exact allow-list/u);
  }
  for (const permission of target.permissions) {
    const capability = fixtureCapability();
    capability.permissions = capability.permissions.filter(value => value !== permission);
    assert.throws(() => checkContract([capability], fixtureConfig()), /exact native allow-list/u);
  }
});

test('checks additional files and inline config grants instead of only default.json', () => {
  const extra = { ...fixtureCapability(), identifier: 'extra', windows: ['web-*'] };
  assert.throws(() => checkContract([fixtureCapability(), extra], fixtureConfig()), /forbidden window/u);
  const config = fixtureConfig();
  config.app.security = { capabilities: ['default', extra] };
  assert.throws(() => checkContract([fixtureCapability()], config), /forbidden window/u);
  config.app.security.capabilities = ['unknown'];
  assert.throws(() => checkContract([fixtureCapability()], config), /Unknown capability/u);
  config.app.security.capabilities = [];
  assert.throws(() => checkContract([fixtureCapability()], config), /must not disable/u);
  config.app.windows.push({ label: 'web-1' });
  delete config.app.security;
  assert.throws(() => checkContract([fixtureCapability()], config), /forbidden app window/u);
});

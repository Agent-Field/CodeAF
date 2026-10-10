import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import ts from 'typescript';

// BE-SEC-17 requires equality, so an unused native handler fails alongside a
// renderer call with no handler. Cargo compilation belongs to CI, not this law.
function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = new URL(entry.name, directory);
    if (entry.isDirectory()) return sourceFiles(new URL(`${entry.name}/`, directory));
    return /\.[cm]?[jt]sx?$/u.test(entry.name) ? [path] : [];
  });
}

function invokedCommands(source, filename = 'fixture.tsx') {
  const tree = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true);
  const names = new Set(['invoke']);
  const commands = new Set();
  for (const statement of tree.statements) {
    if (!ts.isImportDeclaration(statement) || statement.moduleSpecifier.text !== '@tauri-apps/api/core') continue;
    const bindings = statement.importClause?.namedBindings;
    if (bindings && ts.isNamedImports(bindings)) {
      for (const binding of bindings.elements) {
        if ((binding.propertyName ?? binding.name).text === 'invoke') names.add(binding.name.text);
      }
    }
  }
  function collect(argument) {
    if (!argument) return;
    if (ts.isStringLiteral(argument) || ts.isNoSubstitutionTemplateLiteral(argument)) {
      commands.add(argument.text);
    } else if (ts.isConditionalExpression(argument)) {
      // Permission queries choose between two native commands at runtime.
      collect(argument.whenTrue);
      collect(argument.whenFalse);
    } else if (ts.isParenthesizedExpression(argument)) {
      collect(argument.expression);
    }
    // Bridge adapters forward a command parameter; their callers name it.
  }
  function visit(node) {
    if (ts.isCallExpression(node)) {
      const callee = node.expression;
      if ((ts.isIdentifier(callee) && names.has(callee.text)) ||
          (ts.isPropertyAccessExpression(callee) && callee.name.text === 'invoke') ||
          (ts.isElementAccessExpression(callee) && ts.isStringLiteral(callee.argumentExpression) &&
            callee.argumentExpression.text === 'invoke')) collect(node.arguments[0]);
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  return commands;
}

function nativeDeclarations(source) {
  // Remove comments and quoted strings so examples cannot supply phantom
  // registrations or modules. Inline modules do not need a disk file.
  const code = source.replace(/"(?:\\.|[^"\\])*"|\/\/[^\n]*|\/\*[\s\S]*?\*\//gu,
    ' ');
  const handlers = new Set();
  const blocks = [...code.matchAll(/\bgenerate_handler\s*!\s*\[([^\]]*)\]/gu)];
  assert.ok(blocks.length > 0, 'lib.rs must register native commands with generate_handler!');
  for (const [, block] of blocks) {
    for (const entry of block.split(',').map(value => value.trim()).filter(Boolean)) {
      assert.match(entry, /^(?:[A-Za-z_]\w*\s*::\s*)*[A-Za-z_]\w*$/u,
        `Unrecognized native handler: ${entry}`);
      handlers.add(entry.split(/\s*::\s*/u).at(-1));
    }
  }
  assert.ok(handlers.size > 0, 'generate_handler! must not be empty');
  const modules = [...code.matchAll(/\bmod\s+([A-Za-z_]\w*)\s*;/gu)].map(match => match[1]);
  return { handlers, modules };
}

function checkCommands(invoked, registered) {
  const missing = [...invoked].filter(name => !registered.has(name)).sort();
  const unused = [...registered].filter(name => !invoked.has(name)).sort();
  assert.deepEqual({ missing, unused }, { missing: [], unused: [] },
    'Native command sets differ: missing = invokes without handlers; unused = handlers without invokes');
}

function checkModules(modules, directory, fileExists = existsSync) {
  for (const name of modules) {
    assert.ok(fileExists(new URL(`${name}.rs`, directory)) ||
      fileExists(new URL(`${name}/mod.rs`, directory)),
    `lib.rs: mod ${name}; needs ${name}.rs or ${name}/mod.rs`);
  }
}

test('BE-SEC-17: every renderer invoke equals a registered native handler', () => {
  const invoked = new Set(sourceFiles(new URL('../src/', import.meta.url))
    .flatMap(path => [...invokedCommands(readFileSync(path, 'utf8'), path.pathname)]));
  assert.ok(invoked.size > 0, 'desktop/src must contain native invokes');
  const { handlers } = nativeDeclarations(readFileSync(new URL('../src-tauri/src/lib.rs', import.meta.url), 'utf8'));
  checkCommands(invoked, handlers);
});

test('every external Rust module declared in lib.rs has a source file', () => {
  const directory = new URL('../src-tauri/src/', import.meta.url);
  const { modules } = nativeDeclarations(readFileSync(new URL('lib.rs', directory), 'utf8'));
  checkModules(modules, directory);
});

test('collects typed, aliased, member and conditional invokes without reading comments or strings', () => {
  const source = `
    import { invoke as nativeCall } from '@tauri-apps/api/core';
    invoke('plain'); invoke<{ value: Array<string> }>("typed");
    nativeCall('aliased'); bridge.invoke<unknown>('member');
    core['invoke']('indexed'); invoke(\`template\`);
    bridge.invoke<Permission>(ask ? 'request' : ('permission'));
    invoke('plain'); nativeCall(command, args);
    // invoke('comment');
    /* invoke('block_comment'); */
    const example = "invoke('string_example')";
  `;
  assert.deepEqual([...invokedCommands(source)].sort(),
    ['aliased', 'indexed', 'member', 'permission', 'plain', 'request', 'template', 'typed']);
});

test('rejects a stray invoke and an uncalled handler with named diagnostics', () => {
  const { handlers } = nativeDeclarations('tauri::generate_handler![native::known,]');
  checkCommands(invokedCommands("invoke('known');"), handlers);
  assert.throws(() => checkCommands(invokedCommands("invoke('known'); invoke('stray');"), handlers),
    error => error.actual.missing.includes('stray'));
  assert.throws(() => checkCommands(new Set(), handlers),
    error => error.actual.unused.includes('known'));
});

test('reads all handler blocks and conditional public modules, excluding inline modules and comments', () => {
  const { handlers, modules } = nativeDeclarations(`
    // mod absent; tauri::generate_handler![fake]
    /* mod missing; tauri::generate_handler![also_fake] */
    let example = "mod fictional; tauri::generate_handler![fictional]";
    #[cfg(target_os = "macos")] pub mod menu;
    mod native;
    mod tests { fn test() {} }
    tauri::generate_handler![engine_connection, native::open_path,]
    tauri::generate_handler![native :: reveal_path]
  `);
  assert.deepEqual([...handlers].sort(), ['engine_connection', 'open_path', 'reveal_path']);
  assert.deepEqual(modules, ['menu', 'native']);
  assert.throws(() => nativeDeclarations('// generate_handler![fake]'), /must register/u);
  assert.throws(() => nativeDeclarations('generate_handler![]'), /must not be empty/u);
});

test('accepts both Rust module layouts and rejects a missing module', () => {
  const directory = new URL('file:///fixture/');
  const files = new Set(['file:///fixture/native.rs', 'file:///fixture/menu/mod.rs']);
  const fileExists = path => files.has(path.href);
  checkModules(['native', 'menu'], directory, fileExists);
  assert.throws(() => checkModules(['absent'], directory, fileExists),
    /mod absent; needs absent\.rs or absent\/mod\.rs/u);
});

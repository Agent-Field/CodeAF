import { readFileSync, readdirSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { design, generatedFiles } from './design-output.mjs';
import { inspectSource, inspectCss } from './design-policy.mjs';
const errors = [];
const read = path => readFileSync(path, 'utf8');
for (const [path, expected] of Object.entries(generatedFiles)) {
 if (read(path) !== expected) errors.push(`${path}: generated output is stale. Run npm run design:generate.`);
}
function walk(dir) {
 for (const entry of readdirSync(dir, { withFileTypes: true })) {
  const path = `${dir}/${entry.name}`;
  if (entry.isDirectory()) walk(path);
  else if (/\.tsx?$/.test(path)) errors.push(...inspectSource(path, read(path)));
  else if (path.endsWith('.css') && path !== 'src/styles/tokens.css') errors.push(...inspectCss(path, read(path), design));
 }
}
walk('src');
const lightKeys = Object.keys(design.themes.light).sort();
if (JSON.stringify(lightKeys) !== JSON.stringify(Object.keys(design.themes.dark).sort())) errors.push('tokens.json: Light and Dark must define the same semantic colors.');
if (!design.tints.hues[design.tints.default]) errors.push('tokens.json: the default tint must be one of tints.hues.');
const metadata = read('index.html');
for (const required of [`href="/favicon.svg"`, `href="/mask-icon.svg"`, `color="${design.brand.background}"`, `content="${design.themes.light['chrome-canvas']}"`]) {
 if (!metadata.includes(required)) errors.push(`index.html: missing central brand/theme metadata ${required}`);
}
try {
 const manifest = JSON.parse(read('src-tauri/icons/brand-manifest.json'));
 const hash = path => createHash('sha256').update(readFileSync(path)).digest('hex');
 if (manifest.source !== hash('src-tauri/icons/source.svg')) errors.push('Native brand artwork is stale. Run npm run brand:icons.');
 for (const [path, expected] of Object.entries(manifest.files)) if (hash(path) !== expected) errors.push(`${path}: differs from the generated brand artwork. Run npm run brand:icons.`);
} catch { errors.push('Native brand manifest missing or invalid. Run npm run brand:icons.'); }
if (errors.length) { console.error(errors.join('\n')); process.exit(1); }
console.log('Design policy passed: centralized tokens, single icon family, shared controls, and matching brand assets.');

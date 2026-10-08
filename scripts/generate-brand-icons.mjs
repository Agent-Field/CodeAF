import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import './generate-design.mjs';
execFileSync('npx', ['tauri', 'icon', 'src-tauri/icons/source.svg'], { stdio: 'inherit' });
const hash = path => createHash('sha256').update(readFileSync(path)).digest('hex');
const files = {};
function walk(dir) {
 for (const entry of readdirSync(dir, { withFileTypes: true })) {
  const path = `${dir}/${entry.name}`;
  if (entry.isDirectory()) walk(path);
  else if (/\.(png|icns|ico)$/.test(path)) files[path] = hash(path);
 }
}
walk('src-tauri/icons');
writeFileSync('src-tauri/icons/brand-manifest.json', JSON.stringify({ source: hash('src-tauri/icons/source.svg'), files }, null, 2) + '\n');

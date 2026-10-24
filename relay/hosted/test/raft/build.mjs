// Bundling the existing relay together with the reactor host measures the full
// package rather than comparing a naked wasm file against the Worker limit.
import { build } from 'esbuild';
import { readFile, writeFile, mkdir, readdir } from 'node:fs/promises';
import { resolve, relative } from 'node:path';
import { gzipSync } from 'node:zlib';
import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
import { dirname } from 'node:path';
import { wasiPreview1 } from '../../src/raft/wasi.js';
const out = resolve(process.argv[2] ?? '/tmp/codeaf-p03');
await mkdir(out, { recursive: true });
const require=createRequire(import.meta.url);
await writeFile(resolve(out,'workerd-path'),require('workerd').default+'\n');
await writeFile(resolve(out,'workerd-schema-path'),resolve(dirname(require.resolve('workerd/package.json')),'workerd.capnp')+'\n');
const result = await build({ entryPoints: [process.env.CODEAF_RAFT_ACTORS ? 'test/raft/actor-worker.js' : 'test/raft/worker.js'], bundle: true, format: 'esm', target: 'es2022', external: ['*.wasm', 'cloudflare:*'], write: false });
const js = result.outputFiles[0].contents;
const production = await build({ entryPoints: ['src/raft-worker.js'], bundle: true, format: 'esm', target: 'es2022', external: ['*.wasm', 'cloudflare:*'], write: false });
const productionJS = production.outputFiles[0].contents;
await writeFile(resolve(out, 'production-worker.js'), productionJS);
await writeFile(resolve(out, 'worker.js'), js);
const storage = await readFile('test/raft/storage-worker.js');
await writeFile(resolve(out,'storage-worker.js'),storage);
const wasm = await readFile(resolve(out, 'core.wasm'));
const imports = [...new Set(WebAssembly.Module.imports(new WebAssembly.Module(wasm)).map(x => {
  if (x.module !== 'wasi_snapshot_preview1' || x.kind !== 'function' || typeof wasiPreview1().imports[x.name] !== 'function') throw new Error('R14 unsupported wasm import');
  return x.name;
}))].sort();
const sha = b => createHash('sha256').update(b).digest('hex');
const source = [];
async function walk(path) {
  for (const entry of (await readdir(path, { withFileTypes: true })).sort((a,b)=>a.name.localeCompare(b.name))) {
    const p = resolve(path, entry.name);
    if (entry.isDirectory()) await walk(p);
    else if ((/\.(go|js|json|mod|sum|patch)$/.test(p) && !p.endsWith('_test.go')) || entry.name === 'LICENSE') source.push([relative(resolve('../..'), p), sha(await readFile(p))]);
  }
}
for (const p of ['../../internal/raftsnapshot', '../../internal/raftcore', '../../internal/raftabi', '../../internal/clusterstate', '../../cmd/codeaf-raft-wasm', '../../third_party/raft', '../../third_party/raft-patch', 'src/raft']) await walk(resolve(p));
for (const p of ['../../go.mod','../../go.sum']) source.push([relative(resolve('../..'),resolve(p)),sha(await readFile(p))]);
source.sort((a,b)=>a[0].localeCompare(b[0]));
const manifest = { abi: 1, coreProtocol: 1, rowSchema: 1, snapshotSchema: 1, snapshotFormatSHA256: sha(await readFile('../../internal/raftsnapshot/format.go')), go: 'go1.26.5', sourceHash: sha(JSON.stringify(source)), wasmSHA256: sha(wasm), sources: source };
await writeFile(resolve(out, 'manifest.json'), JSON.stringify(manifest, null, 2)+'\n');
const compressed = { wasm: gzipSync(wasm,{level:9}).length, javascript: Math.max(gzipSync(js,{level:9}).length, gzipSync(productionJS,{level:9}).length), storageFixture:gzipSync(storage,{level:9}).length, manifest: gzipSync(JSON.stringify(manifest),{level:9}).length };
const report = { imports, rawWasm: wasm.length, rawJavascript: js.length, rawProductionJavascript: productionJS.length, productionJavascriptCompressed: gzipSync(productionJS,{level:9}).length, compressed, totalCompressed: Object.values(compressed).reduce((a,b)=>a+b,0), limit: 3*1024*1024, wasmSHA256: manifest.wasmSHA256, sourceHash: manifest.sourceHash };
await writeFile(resolve(out, 'size.json'), JSON.stringify(report, null, 2)+'\n');
console.log(JSON.stringify(report));
if (report.totalCompressed >= report.limit) throw new Error('R27 full package exceeds 3 MiB');

import test from 'node:test';
import assert from 'node:assert/strict';
import { Writer, Reader, frame, response, MAX_BUFFER } from '../../src/raft/abi.js';
import { wasiPreview1 } from '../../src/raft/wasi.js';

test('R14 binary ABI preserves full uint64 and opaque bytes',()=>{
 const bytes=new Writer().u32(1).u64((1n<<64n)-1n).blob(Uint8Array.from({length:256},(_,i)=>i)).str('opaque').finish();
 const r=new Reader(bytes);assert.equal(r.u32(),1);assert.equal(r.u64(),(1n<<64n)-1n);assert.deepEqual(r.blob(),Uint8Array.from({length:256},(_,i)=>i));assert.equal(r.str(),'opaque');r.end();
 assert.throws(()=>r.u32(),/truncated/);assert.throws(()=>response(new Writer().u32(2).str('').finish()),/version/);
 assert.throws(()=>new Writer().u32(-1),/uint32/);assert.throws(()=>new Writer().u64(-1n),/uint64/);assert.throws(()=>new Writer().u64(1n<<64n),/uint64/);assert.throws(()=>new Writer().blob('bytes'),/bytes required/);
 assert.equal(frame(1).finish().length,8);assert.equal(MAX_BUFFER,2*1024*1024);
});

test('R27 WASI shim has fixed environment, no I/O and rejects blocking scheduling',()=>{
 const wasi=wasiPreview1(),memory=new WebAssembly.Memory({initial:1});wasi.attach(memory);const v=new DataView(memory.buffer);
 assert.equal(wasi.imports.environ_sizes_get(0,4),0);assert.equal(v.getUint32(0,true),3);
 const size=v.getUint32(4,true);assert.equal(wasi.imports.environ_get(8,32),0);
 assert.deepEqual([...Array(3)].map((_,i)=>v.getUint32(8+i*4,true)),[32,49,57]);
 assert.equal(new TextDecoder().decode(new Uint8Array(memory.buffer,32,size)),'GOMEMLIMIT=48MiB\0GOGC=50\0GOMAXPROCS=1\0');
 assert.throws(()=>wasi.imports.poll_oneoff(),/blocking poll/);assert.throws(()=>wasi.imports.sched_yield(),/scheduler yield/);
 assert.equal(wasi.imports.fd_read(),8);assert.equal(wasi.imports.sock_accept(),8);assert.equal(wasi.imports.fd_write(9),8);
 assert.throws(()=>wasi.imports.random_get(65536,1),/bounds/);
});

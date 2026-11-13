import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import { hostedFixture } from './hosted-fixture.js';

const root=new URL('../../../../',import.meta.url);
const temp=await mkdtemp(join(tmpdir(),'codeaf-hosted-journey-')),binary=join(temp,'client.test');
test.after(()=>rm(temp,{recursive:true,force:true}));
const footprint=new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname;
async function prepare(args){
 const child=spawn('sh',[footprint,...args],{cwd:root,stdio:['ignore','pipe','pipe']});
 let output='';child.stdout.on('data',b=>{output+=b});child.stderr.on('data',b=>{output+=b});
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});
 console.log(output);assert.equal(code,0,output);
}
// R27: compilation can leave a disk backlog after initial admission. Compile
// before starting workerd and admit the actual runtime separately, so package
// linking cannot consume the journey's ordinary network request deadlines.
await prepare(['env','GOMEMLIMIT=192MiB','go','test','-p=1','./internal/raftdirectory','-c','-o',binary]);
await prepare(['true']);

// H4/R1/R2/R23/R25/R29 run ordinary Go Sync clients and the unchanged real
// handoff assertions through the public Worker, with two disposable homes.
test('H4 R1 R2 R23 R25 R29 two Go homes continue, take back, quit, switch off and revoke through workerd',{timeout:180000},async()=>{
 const h=await hostedFixture();
 try{
  await h.start();const url=await h.mf.ready;assert.equal(url.hostname,'127.0.0.1');
  const child=spawn(binary,['-test.run=^TestR1R2R23R25R29HostedTwoHomes$','-test.count=1','-test.timeout=120s','-test.v'],{cwd:new URL('../../../../internal/raftdirectory/',import.meta.url),env:{...process.env,CODEAF_HOSTED_PROOF_URL:url.origin,GOMEMLIMIT:'192MiB'},stdio:['ignore','pipe','pipe']});
  let output='';child.stdout.on('data',b=>{output+=b;});child.stderr.on('data',b=>{output+=b;});
  const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve);});
  console.log(output);assert.equal(code,0,output);assert.match(output,/--- PASS: TestR1R2R23R25R29HostedTwoHomes/);assert.doesNotMatch(output,/--- SKIP:/);
 }finally{await h.close();}
});

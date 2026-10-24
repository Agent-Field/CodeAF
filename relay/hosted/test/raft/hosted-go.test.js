import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { hostedFixture } from './hosted-fixture.js';

// H4/R1/R2/R23/R25/R29 run ordinary Go Sync clients and the unchanged real
// handoff assertions through the public Worker, with two disposable homes.
test('H4 R1 R2 R23 R25 R29 two Go homes continue, take back, quit, switch off and revoke through workerd',{timeout:180000},async()=>{
 const h=await hostedFixture();
 try{
  await h.start();const url=await h.mf.ready;assert.equal(url.hostname,'127.0.0.1');
  const child=spawn('go',['test','-p=1','./internal/raftdirectory','-run','^TestR1R2R23R25R29HostedTwoHomes$','-count=1','-timeout=120s','-v'],{cwd:new URL('../../../../',import.meta.url),env:{...process.env,CODEAF_HOSTED_PROOF_URL:url.origin,GOMEMLIMIT:'192MiB'},stdio:['ignore','pipe','pipe']});
  let output='';child.stdout.on('data',b=>{output+=b;});child.stderr.on('data',b=>{output+=b;});
  const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve);});
  console.log(output);assert.equal(code,0,output);assert.match(output,/--- PASS: TestR1R2R23R25R29HostedTwoHomes/);assert.doesNotMatch(output,/--- SKIP:/);
 }finally{await h.close();}
});

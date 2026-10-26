import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {join} from 'node:path';
import {hostedFixture} from './hosted-fixture.js';
const root=new URL('../../../../',import.meta.url),footprint=new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname;
async function run(args,env={}){
 const child=spawn('sh',[footprint,...args],{cwd:root,env:{...process.env,...env},stdio:['ignore','pipe','pipe']});let output='';
 child.stdout.on('data',b=>{output+=b;process.stdout.write(b);});child.stderr.on('data',b=>{output+=b;process.stderr.write(b);});
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});return {code,output};
}
const binary=join(process.env.CODEAF_RAFT_PROOF_DIR,'hosted-quiet.test');
const compiled=await run(['go','test','-c','-p=1','-timeout','15m','-o',binary,'./internal/syncsetup']);assert.equal(compiled.code,0,compiled.output);
for(const name of ['TestR23R26R27C30HostedOpenHomesIdleEvictionAndWake','TestR23HostedJoinedLearnerQuietGuards','TestR23HostedQuietHomesMakeNoClusterRequests']){
 test('R23 R26 R27 C30 hosted real idle client '+name,{timeout:300000},async()=>{
  const h=await hostedFixture({fixture:true});
  try{
   await h.start();const url=await h.mf.ready;
   const {code,output}=await run([binary,'-test.run','^'+name+'$','-test.count=1','-test.timeout=15m','-test.parallel=1','-test.v'],{CODEAF_HOSTED_PROOF_URL:url.origin});
   assert.equal(code,0,output);assert.doesNotMatch(output,/--- SKIP:/);
   if(name==='TestR23HostedQuietHomesMakeNoClusterRequests')assert.equal((await h.control({op:'metrics'})).http,0);
  }finally{await h.close();}
 });
}

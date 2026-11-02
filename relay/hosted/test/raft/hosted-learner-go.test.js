import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {join} from 'node:path';
import {hostedFixture} from './hosted-fixture.js';
const root=new URL('../../../../',import.meta.url),footprint=new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname;
async function run(args,env={}){
 const child=spawn('sh',[footprint,...args],{cwd:root,env:{...process.env,...env},stdio:['ignore','pipe','pipe']});let output='';
 child.stdout.on('data',b=>output+=b);child.stderr.on('data',b=>output+=b);
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});return {code,output};
}
const binary=join(process.env.CODEAF_RAFT_PROOF_DIR,'hosted-client.test');
const compiled=await run(['go','test','-c','-p=1','-timeout','15m','-o',binary,'./internal/syncsetup']);console.log(compiled.output);assert.equal(compiled.code,0,compiled.output);
// R9/R23 use the production Worker and the terminal's real state owner.
// Independent identities use independent runtime lifetimes so completed cases
// do not consume the next case's active-member memory reservation.
for(const name of ['TestR9R23HostedLearnerRunStateReopenAndWipe','TestR23HostedQuietHomesMakeNoClusterRequests','TestR9HostedLearnerSnapshotChunksAndLog','TestR9R27HostedLearnerMaximumCommand']){
 test('R9 R23 hosted real client '+name,{timeout:300000},async()=>{
  const h=await hostedFixture({base:'/fabric'});
  try{
   await h.start();const url=await h.mf.ready;
   const {code,output}=await run([binary,'-test.run','^'+name+'$','-test.count=1','-test.timeout=15m','-test.parallel=1','-test.v'],{CODEAF_HOSTED_PROOF_URL:url.origin+'/fabric'});
   console.log(output);assert.equal(code,0,output);assert.doesNotMatch(output,/--- SKIP:/);
  }finally{await h.close();}
 });
}

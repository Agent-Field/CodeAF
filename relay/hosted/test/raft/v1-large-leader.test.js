import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {hostedFixture} from './hosted-fixture.js';

const root=new URL('../../../../',import.meta.url),dir=await mkdtemp(join(tmpdir(),'codeaf-large-leader-')),binary=join(dir,'client.test');
async function run(args,env={}){
 const child=spawn('go',args,{cwd:root,env:{...process.env,...env},stdio:['ignore','pipe','pipe']});let output='';
 child.stdout.on('data',b=>{output+=b});child.stderr.on('data',b=>{output+=b});
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});
 console.log(output);assert.equal(code,0,output);return output;
}
try{
 await run(['test','-p=1','./internal/syncsetup','-c','-o',binary]);
 const admission=spawn('sh',[new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname,'true'],{cwd:root,stdio:'inherit'});
 assert.equal(await new Promise((resolve,reject)=>{admission.once('error',reject);admission.once('exit',resolve)}),0);

 // R25/R6 keep the original HTTP and forwarding deadlines while a real
 // personal core leads the hosted voters and returns complete large views.
 test('R25 large legacy requests and replies through a personal workerd leader',{timeout:180000},async()=>{
  const h=await hostedFixture({fixture:true});try{
   await h.start();const url=await h.mf.ready;
   const child=spawn(binary,['-test.run=^TestR25HostedPersonalLeaderLargeLegacyRequests$','-test.count=1','-test.timeout=15m','-test.v'],{cwd:new URL('../../../../internal/syncsetup/',import.meta.url),env:{...process.env,CODEAF_HOSTED_PROOF_URL:url.origin},stdio:['ignore','pipe','pipe']});let output='';
   child.stdout.on('data',b=>{output+=b});child.stderr.on('data',b=>{output+=b});
   const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});
   console.log(output);if(code!==0){const metrics=await h.control({op:'metrics'});console.log(JSON.stringify({leaderFailureMetrics:{peakMemory:metrics.peakMemory,active:metrics.active},progress:await h.control({op:'progress-state',identity:/leader identity (id_[a-f0-9]+)/.exec(output)?.[1]??h.identity})}));}assert.equal(code,0,output);assert.match(output,/--- PASS: TestR25HostedPersonalLeaderLargeLegacyRequests/);assert.doesNotMatch(output,/--- SKIP:/);
  }finally{await h.close();}
 });
 test.after(()=>rm(dir,{recursive:true,force:true}));
}catch(error){await rm(dir,{recursive:true,force:true});throw error;}

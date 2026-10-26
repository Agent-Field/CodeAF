import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {hostedFixture} from './hosted-fixture.js';
const root=new URL('../../../../',import.meta.url);
const footprint=new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname;
async function run(args,env={},signal){
 const child=spawn('sh',[footprint,...args],{cwd:root,env:{...process.env,...env},stdio:['ignore','pipe','pipe'],signal});
 let output='';child.stdout.on('data',b=>{output+=b;process.stdout.write(b)});child.stderr.on('data',b=>{output+=b;process.stdout.write(b)});
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});assert.equal(code,0,output);return output;
}
const dir=await mkdtemp(join(tmpdir(),'codeaf-lead-proof-')),binary=join(dir,'client.test');
try{
 await run(['go','test','-p=1','-timeout=15m','./internal/syncsetup','-c','-o',binary]);
 // Each identity proof gets a fresh isolate, retaining the shared admission cap.
 for(const name of [
  'TestR7HostedPersonalLeaderFallbackAndNoAutomaticRegain',
  'TestR7HostedPersonalLeaderProcessDeathRestoresAnchors',
  'TestR8HostedPersonalLeaderAndAnchorLossNeverCreatesSecondCluster',
  'TestR26HostedPersonalLeaderColdReadKeepsQuorum',
  'TestR7R26HostedColdSleepingLeaderDepartureRestoresAnchors',
  'TestR23R26R27HostedLeadingHomesIdleEvictionAndWake',
  'TestR7R23R26R27HostedSyncOffFallbackIdleEvictionAndWake',
 ]){
  test('R7 R8 R23 R26 '+name,{timeout:300000},async t=>{
   const h=await hostedFixture({fixture:true});try{
    await h.start();const url=await h.mf.ready;
    const output=await run([binary,'-test.run=^'+name+'$','-test.count=1','-test.timeout=15m','-test.v'],{CODEAF_HOSTED_PROOF_URL:url.origin},t.signal);
    assert.doesNotMatch(output,/--- SKIP:/);assert.match(output,new RegExp('--- PASS: '+name));
   }finally{await h.close();}
  });
 }
 test.after(()=>rm(dir,{recursive:true,force:true}));
}catch(error){await rm(dir,{recursive:true,force:true});throw error;}

import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { hostedFixture } from './hosted-fixture.js';

const root=new URL('../../../../',import.meta.url);
const footprint=new URL('../../../../scripts/raft-proof-footprint.sh',import.meta.url).pathname;
async function run(args,env={}){
 const child=spawn('sh',[footprint,...args],{cwd:root,env:{...process.env,...env},stdio:['ignore','pipe','pipe']});
 let output='';child.stdout.on('data',b=>{output+=b});child.stderr.on('data',b=>{output+=b});
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});
 return {code,output};
}
// Linking the client tests can leave a disk backlog, so it finishes before
// the Worker starts and cannot spend the journey's own request deadlines.
const compiled=await run(['go','test','-p=1','./internal/syncsetup','-run','^$','-count=1']);
console.log(compiled.output);assert.equal(compiled.code,0,compiled.output);

// R6/R9/R23 run the real codeaf client against the production Worker entry,
// so the personal-member lifecycle is proved on the default relay itself.
test('R6 R9 R23 a real codeaf home joins the hosted cluster as a learner and leads it',{timeout:300000},async()=>{
 const h=await hostedFixture();
 try{
  await h.start();const url=await h.mf.ready;assert.equal(url.hostname,'127.0.0.1');
  const {code,output}=await run(['go','test','-p=1','./internal/syncsetup','-run','^TestR6R9R23HostedPersonalMemberLifecycle$','-count=1','-timeout=240s','-v'],{CODEAF_HOSTED_PROOF_URL:url.origin});
  console.log(output);
  assert.doesNotMatch(output,/--- SKIP:/);
  assert.equal(code,0,output);assert.match(output,/--- PASS: TestR6R9R23HostedPersonalMemberLifecycle/);
 }finally{await h.close();}
});

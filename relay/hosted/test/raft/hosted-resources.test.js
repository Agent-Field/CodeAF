import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { hostedFixture } from './hosted-fixture.js';

const ticks=Number(execFileSync('getconf',['CLK_TCK'],{encoding:'utf8'}));
async function sample(){
 const ids=[process.pid,...(await readFile(`/proc/${process.pid}/task/${process.pid}/children`,'utf8')).trim().split(/\s+/).filter(Boolean).map(Number)],rows=[];
 for(const pid of ids){try{const status=await readFile(`/proc/${pid}/status`,'utf8'),stat=(await readFile(`/proc/${pid}/stat`,'utf8')).split(') ').slice(1).join(') ').split(' ');rows.push({pid,cpu:(Number(stat[11])+Number(stat[12]))/ticks,rss:Number(status.match(/^VmRSS:\s+(\d+)/m)?.[1]??0)*1024});}catch{}}
 return rows;
}
async function inspect(mf){
 const base=await mf.getInspectorURL();base.protocol='http:';
 const targets=await (await fetch(new URL('/json/list',base))).json(),target=targets.find(t=>t.title?.includes('hosted-proof'))??targets.find(t=>t.title==='Cloudflare Worker');assert(target);
 const ws=new WebSocket(target.webSocketDebuggerUrl);await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject;});let seq=0;const waiting=new Map();
 ws.onmessage=e=>{const m=JSON.parse(e.data),p=waiting.get(m.id);if(!p)return;waiting.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error(JSON.stringify(m.error))):p.resolve(m.result);};
 return {close:()=>ws.close(),call:method=>new Promise((resolve,reject)=>{const id=++seq,timer=setTimeout(()=>reject(Error('inspector timeout')),5000);waiting.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method}));})};
}

// H4/R27 measures the actual public coordinator and all three co-resident
// anchors. The average CPU budget includes source, peers and storage work;
// endpoint wall latency includes loopback waits and is reported separately.
test('H4 R27 public Worker stays within isolate and ordinary-turn CPU budgets',{timeout:30000},async()=>{
 const h=await hostedFixture({fixture:true}),a=h.client(),report={calls:[],heaps:[],samples:[]};let inspector;
 try{
  await h.start();inspector=await inspect(h.mf);
  const begin=performance.now();assert.equal((await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}})).status,200);report.coldMs=performance.now()-begin;
  await h.control({op:'reset-metrics'});const before=await sample();report.samples.push(before);
  for(let i=0;i<24;i++){
   const start=performance.now(),r=await h.call(a,'GET','/v1/dir/cells/chat');assert.equal(r.status,200);report.calls.push(performance.now()-start);
   if(i%4===0){report.samples.push(await sample());report.heaps.push(await inspector.call('Runtime.getHeapUsage'));}
  }
  const after=await sample(),metrics=await h.control({op:'metrics'});report.samples.push(after);report.metrics=metrics;
  const prior=new Map(before.map(p=>[p.pid,p.cpu]));report.cpuMs=after.reduce((n,p)=>n+(p.cpu-(prior.get(p.pid)??p.cpu))*1000,0);report.cpuMsPerTurn=report.cpuMs/metrics.turns;
  report.combinedRSS=Math.max(...report.samples.map(s=>s.reduce((n,p)=>n+p.rss,0)));
  report.isolateBytes=Math.max(...report.heaps.map(h=>h.totalSize+(h.embedderHeapUsedSize??0)+(h.backingStorageSize??0)))+metrics.peakMemory;
  report.calls.sort((a,b)=>a-b);report.endpointWall={p50:report.calls[Math.floor(report.calls.length*.5)],p95:report.calls[Math.floor(report.calls.length*.95)],p99:report.calls.at(-1)};
  const walls=metrics.wall.slice().sort((a,b)=>a-b);report.turnWall={p50:walls[Math.floor(walls.length*.5)],p95:walls[Math.floor(walls.length*.95)],p99:walls.at(-1)};
  console.log(JSON.stringify({cpuMsPerTurn:report.cpuMsPerTurn,isolateBytes:report.isolateBytes,combinedRSS:report.combinedRSS,endpointWall:report.endpointWall,turnWall:report.turnWall}));
  await writeFile(process.env.CODEAF_RAFT_PROOF_DIR+'/hosted-runtime.json',JSON.stringify(report,null,2)+'\n');
  assert.equal(metrics.active,3);assert(report.isolateBytes<=96*1024*1024);assert(metrics.peakMemory<=64*1024*1024);assert(report.cpuMsPerTurn<=5);
  // This is a workerd CPU/isolate proof. Miniflare's SDK/coordinator RSS is
  // reported, while the separate native/workerd gate enforces summed 256 MiB.
 }finally{inspector?.close();await h.close();}
});

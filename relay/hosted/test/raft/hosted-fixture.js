import assert from 'node:assert/strict';
import { prefixOf } from '../../src/base.js';
import { encodeCanonical } from '../../src/raft/codec.js';
import { Miniflare, convertV4MiniflareOptions } from 'miniflare';
import { build } from 'esbuild';
import { mkdtemp, copyFile, rm, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateKeyPairSync, createHash, sign } from 'node:crypto';

const sha=b=>createHash('sha256').update(b).digest('hex');
export async function hostedFixture({fixture=false,base='',go=false}={}) {
  const prefix=prefixOf({BASE_PATH:base});
  const out=process.env.CODEAF_RAFT_PROOF_DIR;assert(out,'build the native/WASI proof first');
  const dir=await mkdtemp(join(tmpdir(),'p17b-'));
  const root=generateKeyPairSync('ed25519'),rootKey=root.publicKey.export({format:'der',type:'spki'}).subarray(-32);
  const identity='id_'+sha(rootKey).slice(0,32);
  const client=()=>{
    const key=generateKeyPairSync('ed25519'),pub=key.publicKey.export({format:'der',type:'spki'}).subarray(-32);
    const cert={V:1,device:pub.toString('hex'),created:1000,sig:''};
    cert.sig=sign(null,Buffer.from('codeaf/device-cert/v1\n'+JSON.stringify(cert)),root.privateKey).toString('hex');
    return {key,cert,device:'dev_'+sha(pub).slice(0,32)};
  };
  await copyFile(join(out,'core.wasm'),join(dir,'core.wasm'));
  let mf;
  const compile=async(cut)=>{
    const plugins=[{name:'local-wasm',setup(b){b.onResolve({filter:/\.wasm$/},()=>({path:'core.wasm',external:true}));}}];
    if(cut)plugins.push({name:'process-cut',setup(b){b.onLoad({filter:/\/src\/raft\/hosted.js$/},async args=>{
      let text=await readFile(args.path,'utf8');const needle=cut.needle;assert.equal(text.split(needle).length,2,'cut must identify exactly one boundary');
      text=text.replace(needle,needle+(cut.after??"throw new Error('H2 injected crash after durable boundary');"));return {contents:text,loader:'js'};
    });}});
    await build({entryPoints:[fixture?'test/raft/hosted-worker.js':'src/raft-worker.js'],bundle:true,format:'esm',target:'es2022',external:['cloudflare:*'],plugins,outfile:join(dir,'worker.js')});
  };
  const start=async(cut)=>{
    if(mf)await mf.dispose();await compile(cut);
    const worker={name:'hosted-proof',modulesRoot:dir,modules:[{type:'ESModule',path:join(dir,'worker.js')},{type:'CompiledWasm',path:join(dir,'core.wasm')}],compatibilityDate:'2026-09-01',durableObjects:Object.fromEntries(Object.entries({IDENTITY:'IdentityDO',RAFT_MEMBERS:'RaftMemberDO',NEWCOMERS:'NewcomerGate',PAIR_GATE:'PairGate',MAILBOX:'Mailbox',LINK_GATE:'LinkGate',LINK_REQUEST:'LinkRequest'}).map(([binding,className])=>[binding,{className,useSQLite:true}])),r2Buckets:['FRAMES'],bindings:{BASE_PATH:base,LEASE_POLICY:'amended',CAF_LIMITS:JSON.stringify({requestsPerMinute:100000,requestsPerMinutePerDevice:100000,minGraceMs:2000})},outboundService:()=>{throw Error('H4 outbound network forbidden');}};
    mf=new Miniflare(convertV4MiniflareOptions({host:'127.0.0.1',inspectorPort:0,resourcePersistencePath:join(dir,'persist'),telemetry:{enabled:false},cf:false,workers:[worker]}));
    await mf.ready;return mf;
  };
  const request=async(c,method,path,body)=>{
    const raw=body===undefined?'':body instanceof Uint8Array?body:JSON.stringify(body),at=String(Date.now());
    const headers={'Codeaf-Identity':rootKey.toString('base64url'),'Codeaf-Cert':Buffer.from(JSON.stringify(c.cert)).toString('base64url'),'Codeaf-Time':at,'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\n'+method+'\n'+prefix+path+'\n'+at+'\n'+sha(raw)),c.key.privateKey).toString('base64url')};
    return mf.dispatchFetch('http://local'+prefix+path,{method,headers,...(method!=='GET'&&raw?{body:raw}:{})});
  };
  const call=async(...args)=>{const response=await request(...args),text=await response.text();return {status:response.status,body:text?JSON.parse(text):null};};
  const control=async value=>{assert(fixture);const r=await mf.dispatchFetch('http://local/fixture',{method:'POST',body:JSON.stringify({identity,...value})});assert.equal(r.status,200,await r.clone().text());return r.json();};
  const api=async(c,q)=>{
    q={identity_key:rootKey.toString('base64'),...q,at:q.at??Date.now(),signature:null};
    if(!q.worker)q.certificate=Buffer.from(JSON.stringify(c.cert)).toString('base64url');
    q.signature=sign(null,Buffer.from('codeaf/cluster-api/1\n'+encodeCanonical(q)),c.key.privateKey).toString('base64');
    const r=await mf.dispatchFetch('http://local'+prefix+'/v2/cluster/api',{method:'POST',headers:{'Codeaf-Identity':rootKey.toString('base64url')},body:encodeCanonical(q)});
    const text=await r.text();return {status:r.status,body:text?JSON.parse(text):null};
  };
  const delegate=certificate=>{
    certificate={v:1,identity,...certificate,signature:null};
    certificate.signature=sign(null,Buffer.from('codeaf/worker-cert/v1\0'+encodeCanonical(certificate)),root.privateKey).toString('base64');return certificate;
  };
  return {identity,client,dir,start,request,call,control,api,delegate,get mf(){return mf;},async close(){if(mf)await mf.dispose();await rm(dir,{recursive:true,force:true});}};
}

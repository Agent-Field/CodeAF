import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, sign, createHash } from 'node:crypto';
import { hostedFixture } from './hosted-fixture.js';
import { frame, rid } from '../helpers.js';
import { ADMIN_DOMAIN } from '../../src/storage.js';

test('R1 R3 R4 R6 R8 R9 production Worker approval, cold replay protection, rotation and revocation', {timeout:60000}, async () => {
  const keys=generateKeyPairSync('ed25519'), pub=keys.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
  const h=await hostedFixture({base:'/fabric',storageOpen:false,adminKeys:pub}), c=h.client();
  let at=Date.now();
  const admin=async(storage,identity=h.identity,stamp=++at)=>{
    const raw=JSON.stringify({v:1,host:'local',identity,storage,at:stamp,nonce:'test-random-'+stamp});
    const signature=sign(null,Buffer.from(ADMIN_DOMAIN+raw),keys.privateKey).toString('base64url');
    const r=await h.mf.dispatchFetch('http://local/fabric/v1/admin/storage',{method:'POST',headers:{'codeaf-admin-signature':signature},body:raw});
    return {status:r.status,body:await r.json()};
  };
  try {
    await h.start();
    // This approval arrives before the namespace has ever authenticated a device.
    const approval=await admin('on');assert.equal(approval.status,200,JSON.stringify(approval));
    const stamp=at;
    assert.equal((await admin('off',h.identity,stamp)).status,409);
    let stats=await h.call(c,'GET','/v1/store/stats');assert.equal(stats.status,200,JSON.stringify(stats));assert.equal(stats.body.storage,'on');
    const f=frame([rid(41)]);
    assert.equal((await h.call(c,'POST','/v1/store/frames',f.bytes)).status,200);
    assert.equal((await admin('off')).status,200);
    stats=await h.call(c,'GET','/v1/store/stats');const used=stats.body.stored_bytes;assert(used>0);
    assert.deepEqual(await h.call(c,'POST','/v1/store/frames',frame([rid(42)]).bytes),{status:403,body:{err:'storage_off'}});
    assert.equal((await h.call(c,'GET','/v1/store/stats')).body.stored_bytes,used);
    assert.equal((await h.request(c,'GET','/v1/store/objects/'+rid(41))).status,200);
    assert.equal((await h.call(c,'GET','/v1/dir/list')).status,200);
    await h.start();
    assert.equal((await admin('on',h.identity,stamp)).status,409);
    assert.equal((await h.call(c,'GET','/v1/store/stats')).body.storage,'off');
    assert.equal((await admin('on')).status,200);
    const nextRoot=generateKeyPairSync('ed25519'),nextDevice=generateKeyPairSync('ed25519');
    const rootBytes=nextRoot.publicKey.export({type:'spki',format:'der'}).subarray(-32);
    const deviceBytes=nextDevice.publicKey.export({type:'spki',format:'der'}).subarray(-32);
    const digest=raw=>createHash('sha256').update(raw).digest('hex');
    const successor='id_'+digest(rootBytes).slice(0,32);
    const cert={V:1,device:deviceBytes.toString('hex'),created:1000,sig:''};
    cert.sig=sign(null,Buffer.from('codeaf/device-cert/v1\n'+JSON.stringify(cert)),nextRoot.privateKey).toString('hex');
    const nextRequest=async(method,path,raw='')=>{
      const stamp=String(Date.now()),uri='/fabric'+path;
      return h.mf.dispatchFetch('http://local'+uri,{method,headers:{
        'Codeaf-Identity':rootBytes.toString('base64url'),'Codeaf-Cert':Buffer.from(JSON.stringify(cert)).toString('base64url'),'Codeaf-Time':stamp,
        'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\n'+method+'\n'+uri+'\n'+stamp+'\n'+digest(raw)),nextDevice.privateKey).toString('base64url')},...(method==='POST'?{body:raw}:{})});
    };
    const handoff={successor,successor_key:rootBytes.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+successor),nextRoot.privateKey).toString('hex')};
    assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',handoff),{status:409,body:{err:'not_rotating'}});
    assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
    assert.equal((await h.call(c,'POST','/v1/store/handoff',{successor})).status,400);
    assert.equal((await h.call(c,'POST','/v1/store/handoff',{...handoff,proof:''})).status,400);
    assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
    assert.equal((await (await nextRequest('GET','/v1/store/stats')).json()).storage,'off');
    assert.equal((await nextRequest('POST','/v1/store/frames',frame([rid(43)]).bytes)).status,403);
    const freeze=await h.call(c,'POST','/v1/store/handoff',handoff);
    assert.deepEqual(freeze,{status:200,body:{successor,storage:'on'}});
    assert.equal((await (await nextRequest('GET','/v1/store/stats')).json()).storage,'on');
    assert.equal((await nextRequest('POST','/v1/store/frames',frame([rid(43)]).bytes)).status,200);
    // A newer direct revocation at the successor outranks inherited approval.
    assert.equal((await admin('off',successor)).status,200);
    const retry=await h.call(c,'POST','/v1/store/handoff',handoff);
    assert.deepEqual(retry,{status:200,body:{successor,storage:'off'}});
    assert.equal((await admin('on',successor,stamp)).status,409);
    assert.equal((await (await nextRequest('GET','/v1/store/stats')).json()).storage,'off');
    const otherRoot=generateKeyPairSync('ed25519'),otherKey=otherRoot.publicKey.export({type:'spki',format:'der'}).subarray(-32);
    const other='id_'+digest(otherKey).slice(0,32);
    const second={successor:other,successor_key:otherKey.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+other),otherRoot.privateKey).toString('hex')};
    await h.start();
    assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'thaw'})).status,200);
    assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',second),{status:409,body:{err:'not_rotating'}});
    assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
    const refused=await h.call(c,'POST','/v1/store/handoff',second);
    assert.deepEqual(refused,{status:409,body:{err:'conflict'}});
    // If inheritance had reached the second identity, this older direct
    // command would be refused as stale instead of succeeding.
    assert.equal((await admin('off',other,stamp)).status,200);
    assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'retire'})).status,200);
    assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',handoff),{status:200,body:{successor,storage:'off'}});
    const statsOff=await admin('off');assert.equal(statsOff.status,200);
  } finally {await h.close();}
});

test('S2 first device after preapproval still pays IP and global newcomer budgets', {timeout:60000}, async () => {
 for(const limits of [{newIdentitiesPerIpPerDay:0},{globalNewIdentitiesPerDay:0}]) {
  const keys=generateKeyPairSync('ed25519'),pub=keys.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
  const h=await hostedFixture({storageOpen:false,adminKeys:pub,limits});
  try {
   await h.start();
   const raw=JSON.stringify({v:1,host:'local',identity:h.identity,storage:'on',at:Date.now(),nonce:'preapprove'});
   const r=await h.mf.dispatchFetch('http://local/v1/admin/storage',{method:'POST',body:raw,headers:{'codeaf-admin-signature':sign(null,Buffer.from(ADMIN_DOMAIN+raw),keys.privateKey).toString('base64url')}});
   assert.equal(r.status,200,await r.text());
   const first=await h.call(h.client(),'GET','/v1/store/stats');
   assert.equal(first.status,429,JSON.stringify(first));
   assert.equal(first.body.err,limits.newIdentitiesPerIpPerDay===0?'too_many_identities':'relay_admission_full');
  } finally { await h.close(); }
 }
});

test('B1 off does not travel and a successor key must match its ID and source-bound proof', {timeout:60000}, async () => {
 const keys=generateKeyPairSync('ed25519'),pub=keys.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
 const h=await hostedFixture({storageOpen:false,adminKeys:pub});
 const at=Date.now();
 const admin=async(storage,identity,stamp)=>{
  const raw=JSON.stringify({v:1,host:'local',identity,storage,at:stamp,nonce:'handoff'});
  return h.mf.dispatchFetch('http://local/v1/admin/storage',{method:'POST',body:raw,headers:{'codeaf-admin-signature':sign(null,Buffer.from(ADMIN_DOMAIN+raw),keys.privateKey).toString('base64url')}});
 };
 try {
  await h.start();const c=h.client();
  assert.equal((await admin('off',h.identity,at+10)).status,200);
  const root=generateKeyPairSync('ed25519'),rawKey=root.publicKey.export({type:'spki',format:'der'}).subarray(-32);
  const successor='id_'+createHash('sha256').update(rawKey).digest('hex').slice(0,32);
  const handoff={successor,successor_key:rawKey.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+successor),root.privateKey).toString('hex')};
  assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
  assert.equal((await h.mf.dispatchFetch('http://local/v1/store/handoff',{method:'POST',body:JSON.stringify(handoff)})).status,401);
  for(const body of [null,[],{successor},{...handoff,successor_key:''},{...handoff,successor:h.identity},{...handoff,successor:'id_'+'e'.repeat(32)}]) {
   assert.equal((await h.call(c,'POST','/v1/store/handoff',body)).status,400,JSON.stringify(body));
  }
  assert.equal((await h.call(c,'POST','/v1/store/handoff',{...handoff,proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\nwrong\n'+successor),root.privateKey).toString('hex')})).status,400);
  assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',handoff),{status:200,body:{successor,storage:'off'}});
  // An inherited off at at+10 would make this older approval stale.
  assert.equal((await admin('on',successor,at)).status,200);
  assert.equal((await h.call(c,'POST','/v1/store/handoff',handoff)).status,200);
  assert.equal((await admin('off',successor,at+1)).status,200);
 }finally{await h.close()}
});

test('S2 real pairing mailbox exchange and machine tunnel bytes while storage is off', {timeout:60000}, async () => {
 const h=await hostedFixture({storageOpen:false,entryPoint:'test/raft/storage-live-worker.js'});
 try{
  await h.start();const client=h.client();
  assert.equal((await h.call(client,'GET','/v1/store/stats')).body.storage,'off');
  const pairKey=Buffer.alloc(16,1).toString('base64url'),joinKey=Buffer.alloc(16,2).toString('base64url');
  const created=await h.mf.dispatchFetch('http://local/v1/pair',{method:'POST',headers:{'codeaf-pair-key':pairKey}});
  assert.equal(created.status,201);const {nameplate}=await created.json();
  const post=async(side,key,bytes)=>h.mf.dispatchFetch('http://local/v1/pair/'+nameplate+'/'+side,{method:'POST',headers:{'codeaf-pair-key':key},body:bytes});
  assert.equal((await post('a',pairKey,'sealed identity grant')).status,201);
  assert.equal((await post('b',joinKey,'paired acknowledgement')).status,201);
  for(const [side,expected]of [['a','sealed identity grant'],['b','paired acknowledgement']]){
   const read=await h.mf.dispatchFetch('http://local/v1/pair/'+nameplate+'/'+side+'?after=0');assert.equal(Buffer.from((await read.json()).msgs[0],'base64').toString(),expected);
  }
  assert.equal((await h.mf.dispatchFetch('http://local/v1/pair/'+nameplate,{method:'DELETE',headers:{'codeaf-pair-key':pairKey}})).status,204);
  const carried=await h.mf.dispatchFetch('http://local/fixture/storage-live');assert.equal(carried.status,200,await carried.clone().text());
  assert.deepEqual(await carried.json(),{registered:true,surface_to_engine:[0,255,19,47],engine_to_surface:[91,0,255]});
  const stats=(await h.call(client,'GET','/v1/store/stats')).body;assert.equal(stats.storage,'off');assert.equal(stats.stored_bytes,0);
  assert.equal((await h.call(client,'POST','/v1/admin/storage',{v:1,host:'local',identity:h.identity,storage:'on',at:Date.now(),nonce:'device-only'})).status,401);
  const bypass=await h.api(client,{action:'setStorage',storage:'on'});assert(bypass.status>=400,JSON.stringify(bypass));
  assert.equal((await h.call(client,'GET','/v1/store/stats')).body.storage,'off');
 }finally{await h.close()}
});

test('R8 globally open storage binds the successor without copying an approval record', {timeout:60000}, async () => {
 const keys=generateKeyPairSync('ed25519'),pub=keys.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
 const h=await hostedFixture({storageOpen:true,adminKeys:pub});
 const at=Date.now();
 const admin=async(storage,identity,stamp)=>{
  const raw=JSON.stringify({v:1,host:'local',identity,storage,at:stamp,nonce:'globally-open'});
  return h.mf.dispatchFetch('http://local/v1/admin/storage',{method:'POST',body:raw,headers:{'codeaf-admin-signature':sign(null,Buffer.from(ADMIN_DOMAIN+raw),keys.privateKey).toString('base64url')}});
 };
 try {
  await h.start();const c=h.client();
  assert.equal((await admin('on',h.identity,at+10)).status,200);
  assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
  const makeHandoff=()=>{
   const root=generateKeyPairSync('ed25519'),rawKey=root.publicKey.export({type:'spki',format:'der'}).subarray(-32);
   const successor='id_'+createHash('sha256').update(rawKey).digest('hex').slice(0,32);
   return {successor,successor_key:rawKey.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+successor),root.privateKey).toString('hex')};
  };
  const handoff=makeHandoff();
  assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',handoff),{status:200,body:{successor:handoff.successor,storage:'on'}});
  // An inherited on at at+10 would make this earlier direct off stale.
  assert.equal((await admin('off',handoff.successor,at)).status,200);
  assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',makeHandoff()),{status:409,body:{err:'conflict'}});
 } finally { await h.close(); }
});

test('R8 a handoff from an unfrozen identity is refused without binding a successor', {timeout:60000}, async () => {
 const h=await hostedFixture({storageOpen:false});
 const makeHandoff=()=>{
  const root=generateKeyPairSync('ed25519'),key=root.publicKey.export({type:'spki',format:'der'}).subarray(-32);
  const successor='id_'+createHash('sha256').update(key).digest('hex').slice(0,32);
  return {successor,successor_key:key.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+successor),root.privateKey).toString('hex')};
 };
 try {
  await h.start();const c=h.client();
  assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',makeHandoff()),{status:409,body:{err:'not_rotating'}});
  assert.equal((await h.call(c,'POST','/v1/identity/rotation',{op:'freeze'})).status,200);
  const accepted=makeHandoff();
  assert.deepEqual(await h.call(c,'POST','/v1/store/handoff',accepted),{status:200,body:{successor:accepted.successor,storage:'off'}});
 } finally {await h.close();}
});


test('R1 only the device that froze the identity can bind its hosted successor', {timeout:60000}, async () => {
 const h=await hostedFixture({storageOpen:false});
 const makeHandoff=()=>{
  const root=generateKeyPairSync('ed25519'),key=root.publicKey.export({type:'spki',format:'der'}).subarray(-32);
  const successor='id_'+createHash('sha256').update(key).digest('hex').slice(0,32);
  return {successor,successor_key:key.toString('hex'),proof:sign(null,Buffer.from('codeaf/storage-handoff/v1\n'+h.identity+'\n'+successor),root.privateKey).toString('hex')};
 };
 try {
  await h.start();const keeper=h.client(),other=h.client();
  for(const device of [keeper,other])assert.equal((await h.call(device,'GET','/v1/store/stats')).body.storage,'off');
  const frozen=await h.call(keeper,'POST','/v1/identity/rotation',{op:'freeze'});
  assert.equal(frozen.status,200);assert.equal(frozen.body.rotation.by,keeper.device);
  const competing=makeHandoff();
  assert.deepEqual(await h.call(other,'POST','/v1/store/handoff',competing),{status:409,body:{err:'not_rotating'}});
  // A durable competing binding would make the keeper's different successor conflict.
  await h.start();const accepted=makeHandoff();
  assert.deepEqual(await h.call(keeper,'POST','/v1/store/handoff',accepted),{status:200,body:{successor:accepted.successor,storage:'off'}});
  assert.deepEqual(await h.call(other,'POST','/v1/store/handoff',competing),{status:409,body:{err:'not_rotating'}});
  assert.equal((await h.call(keeper,'POST','/v1/identity/rotation',{op:'retire'})).status,200);
  assert.deepEqual(await h.call(other,'POST','/v1/store/handoff',accepted),{status:409,body:{err:'not_rotating'}});
  assert.deepEqual(await h.call(keeper,'POST','/v1/store/handoff',accepted),{status:200,body:{successor:accepted.successor,storage:'off'}});
 } finally {await h.close();}
});

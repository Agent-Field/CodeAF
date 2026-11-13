import test from 'node:test';
import assert from 'node:assert/strict';
import {sign, createHash} from 'node:crypto';
import {hostedFixture} from './hosted-fixture.js';

for (const base of ['', '/fabric', '/my-relay', '/a/relay']) {
  test(`${base === '' || base === '/fabric' ? 'r5.2' : 'r5.1'} signed directory operations at ${base || '/'}`, {timeout:30000}, async () => {
    const h=await hostedFixture({base}),a=h.client(),b=h.client();
    try {
      await h.start();
      const list=await h.call(a,'GET','/v1/dir/list');
      assert.equal(list.status,200,JSON.stringify(list));
      assert.deepEqual(list.body.cells,{});
      const created=await h.call(a,'POST','/v1/dir/cells/chat',{head:'saved',class:'work',size:0,keys:{}});
      assert.equal(created.status,200,JSON.stringify(created));
      assert.equal(created.body.cell.lease.fence,1);
      assert.equal(created.body.cell.lease.device,a.device);
      const acquired=await h.call(b,'POST','/v1/dir/cells/chat/acquire',{force:true,expect:{device:a.device,fence:1}});
      assert.equal(acquired.status,200,JSON.stringify(acquired));
      assert.equal(acquired.body.cell.head,'saved');
      assert.equal(acquired.body.cell.lease.fence,2);
      assert.equal(acquired.body.cell.lease.device,b.device);
      assert.equal((await h.call(b,'POST','/v1/dir/cells/chat/release',{fence:2})).status,204);
      const released=await h.call(a,'GET','/v1/dir/cells/chat');
      assert.equal(released.status,200);
      assert.equal(released.body.cell.lease.expires,0);
      assert.equal(released.body.cell.lease.fence,2);
    } finally {await h.close();}
  });
}

test('r5.3 signatures bind the complete configured path and outside paths stay absent', {timeout:30000}, async () => {
  const base='/a/relay',h=await hostedFixture({base}),a=h.client();
  try {
    await h.start();
    const dispatch=h.mf.dispatchFetch.bind(h.mf);
    let captured;
    h.mf.dispatchFetch=(url,init)=>{captured={url,init};return dispatch(url,init);};
    assert.equal((await h.call(a,'GET','/v1/dir/list')).status,200);
    const {headers}=captured.init;
    let r=await dispatch('http://local'+base+'/v1/dir/cells/other',{headers});
    assert.equal(r.status,401);assert.deepEqual(await r.json(),{err:'unauthorized'});
    const at=headers['Codeaf-Time'],hash=createHash('sha256').update('').digest('hex');
    const bare={...headers,'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\nGET\n/v1/dir/list\n'+at+'\n'+hash),a.key.privateKey).toString('base64url')};
    r=await dispatch('http://local'+base+'/v1/dir/list',{headers:bare});
    assert.equal(r.status,401);assert.deepEqual(await r.json(),{err:'unauthorized'});
    for (const path of ['/v1/dir/list',base+'x/v1/dir/list','/a/v1/dir/list']) {
      r=await dispatch('http://local'+path,{headers});
      assert.equal(r.status,404);assert.deepEqual(await r.json(),{err:'not_found'});
    }
  } finally {await h.close();}
});

test('r5.5 a paired computer enrolls under a custom prefix', {timeout:30000}, async () => {
  const h=await hostedFixture({base:'/a/relay'}),a=h.client();
  try {
    await h.start();
    const body={store:'01'.repeat(32),protocol:1};
    for (let n=0;n<2;n++) {
      const r=await h.request(a,'POST','/v2/cluster/enroll',body),text=await r.text();
      assert.equal(r.status,200,text);
      assert.equal(JSON.parse(text).node,'4');
    }
  } finally {await h.close();}
});

test('r5.6 hosted create and publish accept absent, null and empty frame plans', {timeout:30000}, async () => {
  const h=await hostedFixture(),a=h.client();
  try {
    await h.start();
    for (const [name,plan] of [['absent',{}],['null',{frames:null}],['empty',{frames:[]}]]) {
      const path='/v1/dir/cells/'+name;
      const created=await h.call(a,'POST',path,{head:'saved',class:'work',keys:{},size:0,...plan});
      assert.equal(created.status,200,JSON.stringify(created));
      const published=await h.call(a,'POST',path+'/publish',{old_head:'saved',head:'next',fence:1,size:0,class:'work',...plan});
      assert.equal(published.status,200,JSON.stringify(published));
      const read=await h.call(a,'GET',path);
      assert.equal(read.status,200);assert.equal(read.body.cell.head,'next');
      assert.equal(read.body.cell.frames?.length??0,0);
    }
  } finally {await h.close();}
});

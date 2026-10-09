import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
const ids = ['aaaaaaaa00000001','bbbbbbbb00000002','cccccccc00000003'];
test('real workspace asks canonical folder IDs despite opaque bridge snapshot token, and Group applies one shared group', async ({page}) => {
 await installMockEngine(page, {initial:{id:'opaque-bridge-token',sessionFile:`/fixture/history/${ids[0]}/transcript.jsonl`}});
 const asked: string[][]=[];
 await page.route('**/api/engine/history/group-offers', async route => {
  const body=route.request().postDataJSON(); asked.push(body.ids);
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({offers:[{basis:'topic',key:'topic:fixture-drip',ids,title:'Garden drip irrigation'}]})});
 });
 await page.addInitScript(ids=>{
  const tabs=ids.map((chat,index)=>({id:`garden-${index}`,kind:'conversation',title:['Garden emitter spacing','Garden valve schedule','Garden hose layout'][index],titleSource:'manual',draft:'',pinned:false,sessionFile:`/fixture/history/${chat}/transcript.jsonl`}));
  localStorage.setItem('codeaf.desktop.workspace.v1',JSON.stringify({tabs,groups:[],closed:[],activeId:tabs[0].id,recentIds:tabs.map(tab=>tab.id),nextNumber:4}));
 },ids);
 await page.goto('/');
 await expect.poll(()=>asked).toEqual([ids]);
 const pill=page.getByRole('group',{name:'Group suggestion'});
 await expect(pill).toContainText('Garden drip irrigation');
 await pill.getByRole('button',{name:'Group',exact:true}).click();
 await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
 await expect(page.locator('.workspace-tab-group [role=tab]')).toHaveCount(3);
 await expect(pill).toHaveCount(0);
 expect(asked).toEqual([ids]);
});

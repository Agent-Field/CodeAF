import {test,expect} from '@playwright/test';
import {installMockEngine} from './support/mock-engine';
import {NOW,withHistory} from './support/scenarios-history';

for(const theme of ['light','dark']) test(`canonical launch waits before touching activity stamps (${theme})`,async({page})=>{
 await page.clock.setFixedTime(NOW);
 const engine=await installMockEngine(page,withHistory([]));
 const hour=3600000, stamp=NOW.getTime();
 const activity={active:{at:stamp-30*hour,hold:false},old:{at:stamp-13*hour,hold:false},held:{at:stamp-40*hour,hold:true},recent:{at:stamp-3*hour,hold:false},otherPlace:{at:stamp-20*hour,hold:false}};
 await page.addInitScript(({activity,theme})=>{localStorage.setItem('codeaf.desktop.activity.v1',JSON.stringify(activity));localStorage.setItem('codeaf.desktop.theme',theme);},{activity,theme});
 const tab=(id:string,title:string)=>({id,title,titleSource:'manual',kind:'conversation',pinned:false,draft:'',sessionFile:`/mock/${id}/transcript.jsonl`});
 let record:any={key:'now',revision:1,workspace:{schema:1,tabs:[tab('active','Active'),tab('old','Old'),tab('held','Held'),tab('recent','Recent')],groups:[],closed:[],nextNumber:5}};
 let release!:()=>void;const gate=new Promise<void>(resolve=>{release=resolve});let started=false, released=false;
 await page.route('**/workspaces/now*',async route=>{
  const url=new URL(route.request().url());if(!url.pathname.endsWith('/workspaces/now'))return route.fallback();
  if(route.request().method()==='GET'&&!released){started=true;await gate;}
  if(url.searchParams.get('wait')==='1'){
   const after=Number(url.searchParams.get('after'));
   while(!page.isClosed() && record.revision===after) await new Promise(resolve=>setTimeout(resolve,10));
   if(page.isClosed()) return;
  }
  if(route.request().method()==='PUT'){
   const body=route.request().postDataJSON();if(body.revision!==record.revision)return route.fulfill({status:409,json:{code:'conflict',current:record}});
   record={key:'now',revision:record.revision+1,writer:body.writer,workspace:body.workspace};
  }
  return route.fulfill({json:record});
 });
 await page.goto('/');
 await expect.poll(()=>started).toBe(true);
 await page.waitForTimeout(250);
 expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('codeaf.desktop.activity.v1')!))).toEqual(activity);
 expect(engine.calls.some(call=>call.path.endsWith('/history/archive'))).toBe(false);
 // A clock written by another Place while this window loads must also survive.
 await page.evaluate(stamp=>{const a=JSON.parse(localStorage.getItem('codeaf.desktop.activity.v1')!);a.otherLater={at:stamp,hold:false};localStorage.setItem('codeaf.desktop.activity.v1',JSON.stringify(a));},stamp);
 released=true;release();
 await expect(page.getByRole('status').filter({hasText:'Archived 1 tab'})).toBeVisible();
 await expect(page.getByRole('tab',{name:'Old',exact:true})).toHaveCount(0);
 for(const name of ['Active','Held','Recent'])await expect(page.getByRole('tab',{name,exact:true})).toBeVisible();
 await expect.poll(()=>record.workspace.tabs.some((t:any)=>t.id==='old')).toBe(false);
 const saved=await page.evaluate(()=>JSON.parse(localStorage.getItem('codeaf.desktop.activity.v1')!));
 expect(saved.otherPlace).toEqual(activity.otherPlace);expect(saved.otherLater).toEqual({at:stamp,hold:false});
 expect(engine.calls.filter(call=>call.path.endsWith('/history/archive')&&call.body.archived===true)).toHaveLength(1);
});

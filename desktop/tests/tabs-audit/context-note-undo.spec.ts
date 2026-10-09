import { test, expect } from '@playwright/test';
import { installMockEngine, MODEL } from '../ui/support/mock-engine';
import { installMockPlaces } from '../ui/support/mock-places';
for (const theme of ['light','dark']) test(`context Undo exact receipts and refusal ${theme}`,async({page},info)=>{
 const engine=await installMockEngine(page,{initial:{model:MODEL,entries:[{Role:'user',Text:'Use these places.'}]}});
 const places=await installMockPlaces(page);
 await page.addInitScript(t=>{localStorage.setItem('codeaf-theme',t);localStorage.setItem('codeaf.desktop.workspace.v1',JSON.stringify({tabs:[{id:'chat',kind:'conversation',title:'Context review',draft:'',pinned:false,titleSource:'manual',sessionFile:'mock-session-1.jsonl'}],groups:[],closed:[],activeId:'chat',nextNumber:2,recentIds:['chat']}));},theme);
 await page.goto('/');
 await expect.poll(()=>engine.calls.filter(c=>c.path.includes('/sessions')).length).toBeGreaterThan(0);
 const mutations=await page.evaluate(async()=>{
  const {createPlacesClient}=await import('/src/features/places/client.ts');
  const client=createPlacesClient();
  return [await client.createPlace({name:'First context'}),await client.createPlace({name:'Second context'})];
 });
 engine.update({entries:[{Role:'user',Text:'Use these places.'},
  {Role:'aside',AsideKind:'places',Text:'Now also using First context',UndoReceipts:mutations[0].undo},
  {Role:'aside',AsideKind:'places',Text:'Now also using Second context',UndoReceipts:mutations[1].undo},
  {Role:'aside',AsideKind:'places',Text:'Now also using External context'},
  {Role:'aside',AsideKind:'places',Text:'Now also using Expired context',UndoReceipts:['rc_expired']},
 ]});
 const note=(text:string)=>page.locator('.system-note').filter({hasText:text});
 await expect(note('First context').getByRole('button',{name:'Undo',exact:true})).toBeVisible();
 await expect(note('External context').getByRole('button')).toHaveCount(0);
 if(process.env.CODEAF_CONTEXT_EVIDENCE) await page.screenshot({path:`${process.env.CODEAF_CONTEXT_EVIDENCE}/${info.project.name}-${theme}-context-undo.png`});
 await note('First context').getByRole('button',{name:'Undo',exact:true}).click();
 await expect(page.locator('.toast')).toContainText('places changed afterwards');
 expect(places.state().places.map(p=>p.name)).toEqual(['First context','Second context']);
 expect(places.posts('/undo').at(-1)?.body.receipts).toEqual(mutations[0].undo);
 await note('Second context').getByRole('button',{name:'Undo',exact:true}).click();
 await expect(note('Second context').getByRole('button')).toHaveCount(0);
 expect(places.state().places.map(p=>p.name)).toEqual(['First context']);
 await note('Expired context').getByRole('button',{name:'Undo',exact:true}).click();
 await expect(page.locator('.toast').filter({hasText:'can no longer be undone'})).toBeVisible();
 expect(places.posts('/undo').at(-1)?.body.receipts).toEqual(['rc_expired']);
 await page.reload();
 await expect(note('First context').getByRole('button',{name:'Undo',exact:true})).toBeVisible();
 await note('First context').getByRole('button',{name:'Undo',exact:true}).click();
 await expect(note('First context').getByRole('button')).toHaveCount(0);
 expect(places.state().places).toHaveLength(0);
});

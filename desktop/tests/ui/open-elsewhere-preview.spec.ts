import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

for (const theme of ['light', 'dark']) test(`actual other-Place caption refreshes only while hovered (${theme})`, async ({ page }) => {
 await installMockEngine(page, plainReply());
 const chat = (id: string, title: string) => ({ id, title, titleSource: 'manual', kind: 'conversation', sessionFile: '/project/chat.jsonl', pinned: false, draft: '' });
 const state = { tabs: [chat('a', 'Active'), chat('b', 'Shared conversation')], groups: [], closed: [], activeId: 'a', nextNumber: 3, recentIds: ['a', 'b'] };
 await page.addInitScript(({state, theme}) => {
  localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
  localStorage.setItem('codeaf.desktop.theme', theme);
 }, {state, theme});
 // Canonical endpoint fixture represents a real Place's stored-open conversation
 // view. Its close/reopen ledger changes independently of the hovered window.
 let otherOpen = true, reads = 0;
 await page.route('**/workspaces/*/open-elsewhere?*', route => {
  reads++;
  return route.fulfill({json: {places: otherOpen ? [{id:'pl_0000000000000001',name:'Marketing'}] : []}});
 });
 await page.goto('/');
 const tab = page.getByRole('tab', {name:'Shared conversation',exact:true});
 await expect(tab).toBeVisible();
 expect(reads).toBe(0);
 await tab.hover();
 const preview = page.getByRole('group', {name:'Preview of Shared conversation',exact:true});
 const note = preview.locator('.preview-context');
 await expect(note).toHaveText('also open in Marketing');
 expect(await note.evaluate(el => getComputedStyle(el).fontSize)).toBe('11px');
 otherOpen = false;
 await expect(note).toHaveCount(0, {timeout:5000});
 otherOpen = true;
 await expect(note).toHaveText('also open in Marketing', {timeout:5000});
 await page.mouse.move(0,0);
 await expect(preview).toHaveCount(0);
 const stopped = reads;
 await page.waitForTimeout(2300);
 expect(reads).toBe(stopped);
 await page.reload();
 await tab.hover();
 await expect(note).toHaveText('also open in Marketing');
 if (process.env.OTHER_OPEN_SHOTS) await preview.screenshot({path:`${process.env.OTHER_OPEN_SHOTS}/${test.info().project.name}-${theme}.png`});
});

test('two actual Place views close, reload and reopen the same conversation independently', async ({ browser }) => {
 const { installMockPlaces } = await import('./support/mock-places');
 const ids = ['pl_0000000000000001', 'pl_0000000000000002'];
 const chat = (id: string, title: string, path='/project/shared.jsonl') => ({id,title,titleSource:'manual',kind:'conversation',sessionFile:path,pinned:false,draft:''});
 const records = new Map(ids.map((key,i) => [key,{key,revision:1,workspace:{schema:1,tabs:[chat(`active${i}`,'Active','/project/active.jsonl'),chat(`shared${i}`,'Shared conversation')],groups:[],closed:[],nextNumber:3}}]));
 const contexts = await Promise.all([browser.newContext(),browser.newContext()]);
 try {
  const pages = await Promise.all(contexts.map(c=>c.newPage()));
  for (const [i,page] of pages.entries()) {
   const engine = await installMockEngine(page,plainReply());
   const hosts = new Map<string, ReturnType<typeof engine.snapshot>>();
   await page.route("**/api/engine/sessions**", route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/sessions") && route.request().method() === "POST") {
     const body = route.request().postDataJSON();
     const id = body.sessionFile === "/project/shared.jsonl" ? "shared" : "active";
     const snapshot = { ...engine.snapshot(), id, sessionFile: body.sessionFile };
     hosts.set(id, snapshot); return route.fulfill({json:snapshot});
    }
    const id = path.split("/sessions/")[1];
    if (route.request().method() === "GET" && hosts.has(id)) return route.fulfill({json:hosts.get(id)});
    return route.fallback();
   });
   await installMockPlaces(page,{places:[{id:ids[0],name:'Design',lastOpenedAt:'now'},{id:ids[1],name:'Marketing',lastOpenedAt:'now'}]});
   await page.route('**/workspaces/**', async route=>{
    const url=new URL(route.request().url());const parts=url.pathname.split('/');const key=parts[parts.indexOf('workspaces')+1];const current=records.get(key);
    if(!current) return route.fallback();
    if(url.pathname.endsWith('/open-elsewhere')) {
     const pane=current.workspace.tabs.flatMap((t:any)=>t.split?.panes??[t]).find((t:any)=>t.id===url.searchParams.get('pane'));
     const places=ids.filter(id=>id!==key && pane?.kind==='conversation' && records.get(id)?.workspace.tabs.some((t:any)=>(t.split?.panes??[t]).some((p:any)=>p.kind==='conversation'&&p.sessionFile===pane.sessionFile))).map(id=>({id,name:id===ids[1]?'Marketing':'Design'}));
     return route.fulfill({json:{places}});
    }
    if(route.request().method()==='PUT') {
     const body=route.request().postDataJSON();
     if(body.revision!==current.revision) return route.fulfill({status:409,json:{code:'conflict',current}});
     records.set(key,{key,revision:current.revision+1,workspace:body.workspace});
    }
    return route.fulfill({json:records.get(key)});
   });
   await page.goto(`/?place=${ids[i]}`);
   await expect(page.getByRole('tab',{name:'Shared conversation',exact:true})).toBeVisible();
  }
  const [first,second]=pages;
  await first.getByRole('tab',{name:'Shared conversation',exact:true}).hover();
  const note=first.locator('.preview-context');await expect(note).toHaveText('also open in Marketing');
  await second.getByRole('button',{name:'Close Shared conversation',exact:true}).click();
  await expect(note).toHaveCount(0,{timeout:5000});
  await second.reload();
  await expect(second.getByRole('tab',{name:'Shared conversation',exact:true})).toHaveCount(0);
  await second.keyboard.press('Control+Shift+t');
  await expect(second.getByRole('tab',{name:'Shared conversation',exact:true})).toBeVisible();
  await expect.poll(() => records.get(ids[1])?.workspace.tabs.some(t => t.sessionFile === '/project/shared.jsonl')).toBe(true);
  await first.reload();
  await first.bringToFront();
  await first.mouse.move(0,0);
  await first.getByRole('tab',{name:'Shared conversation',exact:true}).hover();
  await expect(note).toHaveText('also open in Marketing',{timeout:5000});
 } finally {await Promise.all(contexts.map(c=>c.close()));}
});

import { test, expect } from '@playwright/test';
import { installMockEngine } from '../ui/support/mock-engine';

const png = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAAFUlEQVR4nGNgKN5HGhrVMKph+GoAAGheMRABgacrAAAAAElFTkSuQmCC';
for (const theme of ['light', 'dark']) {
 test(`all four file glyphs and populated web favicon render in strip/split/address ${theme}`, async ({page}) => {
  await installMockEngine(page, {});
  const reads = new Map<string,number>();
  await page.route('**/api/engine/workspaces/now/favicon?*', route => {
   const id = new URL(route.request().url()).searchParams.get('pane')!;
   reads.set(id,(reads.get(id) ?? 0)+1);
   return route.fulfill({json:{domain:'example.com',dataUrl:id==='missing'?'':png}});
  });
  const base=(id:string,title:string,kind='file')=>({id,title,kind,draft:'',pinned:false});
  await page.addInitScript(({theme,tabs}) => { localStorage.setItem('codeaf-theme',theme); localStorage.setItem('codeaf.desktop.workspace.v1',JSON.stringify({tabs,groups:[],closed:[],activeId:'web',nextNumber:8,recentIds:tabs.map(t=>t.id)})); }, {theme,tabs:[
   base('code','lexer.go'),base('json','package.json'),base('text','README.md'),base('image','logo.png'),
   {...base('web','Example','web'),target:{url:'https://example.com/page'}},
   {...base('missing','Missing icon','web'),target:{url:'https://example.com/missing'}},
   {...base('split','Example split','web'),split:{layout:'1x2',focus:0,panes:[
    {...base('split-web','Split example','web'),target:{url:'https://example.com/split'}},base('split-chat','Split chat','conversation')
   ]}}
  ]});
  await page.goto('/');
  for(const [id,icon] of [['code','fileCode2'],['json','fileJson'],['text','file'],['image','image']]) {
   const mark=page.locator(`#tab-${id} [data-icon="${icon}"]`);
   await expect(mark).toBeVisible();
   await expect(mark).toHaveCSS('width','13px');
  }
  await expect(page.locator('#tab-web img.tab-monogram')).toHaveAttribute('src',png);
  await expect.poll(() => page.locator('#tab-web img.tab-monogram').evaluate((image: HTMLImageElement) => image.naturalWidth)).toBe(16);
  await expect(page.locator('#tab-split-web img.tab-monogram')).toHaveAttribute('src',png);
  await expect(page.locator('.web-address img.tab-monogram')).toHaveAttribute('src',png);
  await expect(page.locator('#tab-missing .tab-monogram')).toHaveText('e');
  await expect(page.locator('#tab-missing img')).toHaveCount(0);
  expect(reads.get('web')).toBe(1); // strip + address share a single read
  await page.screenshot({path:`/home/santosh/.codex/codeaf-design-run/favicon-glyphs-${theme}-${test.info().project.name}.png`});
 });
}

import { createServer, type ServerResponse } from 'node:http';
import { expect, test as base } from '@playwright/test';
import { expectAccessible } from './contracts';
import { withStreamingText } from '../../src/features/chat/engine-stream';
import { emptyDocument, type WorkSection } from '../../src/features/chat/work-model';
import type { EngineAnswer, EngineQuestion, EngineSnapshot } from '../../src/features/chat/engine-client';
const question: EngineQuestion = { id:7,kind:'ask',ask:'choice',head:'Which package should I prepare?',reason:'The release can include one package.',options:[{key:'2',label:'Desktop package',consequence:'Prepare the desktop package.'},{key:'8',label:'Command line package'}],input:{kind:'text',prompt:'Anything to keep in mind?'} };
const test=base.extend<{ engine: {snapshot:EngineSnapshot; answers:EngineAnswer[]; reject:boolean} }>({
 engine:async({page},use)=>{
  const engine={snapshot:{id:'decision-session',sessionFile:'saved-decision.jsonl',workspace:'/workspace',model:'deepseek/deepseek-v4.1-flash',persistent:true,running:true,needsPerson:true,questions:[question],entries:[],tasks:[],usage:{Input:0,Output:0,CostUSD:0,Duration:0,Turns:0},title:'Release preparation',seq:0} as EngineSnapshot,answers:[] as EngineAnswer[],reject:false};
  const readers=new Set<ServerResponse>();
  const server=createServer(async(req,res)=>{
   res.setHeader('Access-Control-Allow-Origin','*');res.setHeader('Access-Control-Allow-Headers','Content-Type,Accept');res.setHeader('Access-Control-Allow-Methods','GET,POST,OPTIONS');
   if(req.method==='OPTIONS'){res.writeHead(204);res.end();return;}
   let input='';for await(const chunk of req)input+=chunk;
   const json=(value:unknown,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
   const path=new URL(req.url??'/', 'http://localhost').pathname;
   if(path.endsWith('/events')){res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(': connected\n\n');readers.add(res);res.on('close',()=>readers.delete(res));return;}
   if(path.endsWith('/answer')){
    engine.answers.push(JSON.parse(input));if(engine.reject){json({error:'This question was already answered elsewhere.'},409);return;}
    engine.snapshot={...engine.snapshot,questions:[],needsPerson:false,seq:engine.snapshot.seq+1};
    readers.forEach(reader=>reader.write(`data: ${JSON.stringify({seq:engine.snapshot.seq,type:'snapshot',snapshot:engine.snapshot})}\n\n`));json({accepted:true});return;
   }
   if(path==='/api/engine/sessions'||path==='/api/engine/sessions/decision-session'){json(engine.snapshot);return;}
   json({error:'Unknown request'},404);
  });
  await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));const address=server.address();if(!address||typeof address==='string')throw new Error('Mock engine did not bind');
  await page.addInitScript(url=>{const original=window.fetch;window.fetch=(input,init)=>typeof input==='string'&&input.startsWith('/api/engine')?original(`${url}${input}`,init):original(input,init);},`http://127.0.0.1:${address.port}`);
  try{await use(engine);}finally{server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));}
 },
});

test('canonical pending choice sends exact identity/key and words; failure preserves both drafts',async({page,engine})=>{
 await page.goto('/');await page.getByRole('button',{name:'Connect engine',exact:true}).click();
 const decision=page.getByRole('region',{name:question.head,exact:true});await expect(decision).toBeVisible();
 const draft=page.getByRole('textbox',{name:/Draft for/});await draft.fill('Unsent instruction stays here');
 const feedback=decision.getByRole('textbox',{name:'Anything to keep in mind?'});await feedback.fill('Keep the current icon family.');
 engine.reject=true;await decision.getByRole('button',{name:'Desktop package',exact:true}).click();
 await expect(decision).toContainText('This question was already answered elsewhere.');
 await expect(feedback).toHaveValue('Keep the current icon family.');await expect(draft).toHaveValue('Unsent instruction stays here');
 engine.reject=false;await decision.getByRole('button',{name:'Desktop package',exact:true}).click();await expect(decision).not.toBeVisible();
 expect(engine.answers).toEqual(Array(2).fill({kind:'ask',id:7,key:'2',picked:['2'],change:'Keep the current icon family.'}));
 await expect(draft).toHaveValue('Unsent instruction stays here');await expectAccessible(page);
});

test('unsupported decision evidence stays visible and cannot silently approve',async({page,engine})=>{
 engine.snapshot.questions=[{...question,attach:[{kind:'image',title:'Design to review',path:'design.png'}]}];
 await page.goto('/');await page.getByRole('button',{name:'Connect engine',exact:true}).click();
 const decision=page.getByRole('region',{name:question.head,exact:true});await expect(decision).toContainText('Design to review');
 await expect(decision).toContainText('Open this conversation in the TUI');await expect(decision.getByRole('button',{name:'Desktop package',exact:true})).toBeDisabled();
 expect(engine.answers).toHaveLength(0);await expectAccessible(page);
});

test('streamed assistant segments preserve recorded paragraphs and tool order through snapshot refresh',()=>{
 const section:WorkSection={...emptyDocument().sections[0],id:'actual-section',title:'Work',original:'Work',digest:'',blocks:[{kind:'paragraph',text:'I will inspect the files.'}],amendments:[],folded:false,originalOpen:false,stepsOpen:false,sample:false,recordedSteps:[{tool:'shell',hint:'Inspect files',args:'ls',output:'src',answered:true}],timeline:[{kind:'text',index:0},{kind:'tool',index:0}]};
 const first=withStreamingText(section,'The files ',1);const next=withStreamingText(first,'The files are ready.',1);
 expect(next.blocks).toEqual([{kind:'paragraph',text:'I will inspect the files.'},{kind:'paragraph',text:'The files are ready.'}]);
 expect(next.timeline).toEqual([{kind:'text',index:0},{kind:'tool',index:0},{kind:'text',index:1}]);
 const snapshotAlreadyAhead=withStreamingText({...section,blocks:next.blocks,timeline:next.timeline},'The files ',1);
 expect(snapshotAlreadyAhead.blocks).toEqual(next.blocks);expect(snapshotAlreadyAhead.timeline).toEqual(next.timeline);
});

test('failed plan reads show the read error instead of claiming no tasks exist',async({page,engine})=>{
 engine.snapshot={...engine.snapshot,planError:'The task plan could not be read.',questions:[],needsPerson:false};
 await page.goto('/');await page.getByRole('button',{name:'Connect engine',exact:true}).click();
 await page.getByRole('button',{name:/^Plan/}).click();
 const plan=page.getByRole('complementary',{name:'Conversation task plan'});
 await expect(plan).toContainText('The task plan could not be read.');
 await expect(plan).not.toContainText('No tasks in the current plan.');
});

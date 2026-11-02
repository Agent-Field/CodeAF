export * from './hosted-worker.js';
export { default } from './hosted-worker.js';
import { IdentityDO as Current } from './hosted-worker.js';
import { Directory } from '../../src/directory.js';
import { checkCert, checkRequest } from '../../src/verify.js';
import { matchRoute } from '../../src/routes.js';
import { readBody } from '../../src/body.js';
import { respond } from '../../src/wire.js';
import { policyOf } from '../../src/rules.js';

// Only the loopback fixture can address the old signed handler. It writes the
// same source tables later frozen by the production first-use upgrade.
export class IdentityDO extends Current {
 fetch(request) {
  if(request.headers.get('Fixture-Legacy')!=='1')return super.fetch(request);
  return respond(async()=>{
   const url=new URL(request.url),route=matchRoute(request.method,url.pathname);
   const headers=Object.fromEntries(['identity','cert','time','sig'].map(k=>[k,request.headers.get('Codeaf-'+k)]));
   const caller=await checkCert(headers),body=await readBody(request,route.limit,route.over);
   const who=await checkRequest(caller,{method:request.method,uri:url.pathname},body,Date.now());
   const dir=new Directory(this.ctx.storage.sql,who.identity,Date.now,policyOf());
   await this.ctx.storage.put({identity:who.identity,admitted:true});
   const answer=await route.handler({tenant:{dir},device:who.device,body,request},route.args);
   await this.ctx.storage.sync();return answer;
  });
 }
}

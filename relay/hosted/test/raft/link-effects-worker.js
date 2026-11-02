// Only this local fixture exposes RPC controls; the public Worker never does.
export { LinkRequest } from '../../src/link/request.js';
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === '/ready') return new Response(null, {status:204});
    const {code,op,args=[]} = await request.json();
    if (!['open','bind','read','decide','deliver'].includes(op)) return new Response(null,{status:404});
    const box=env.LINKS.get(env.LINKS.idFromName(code));
    const answer=await box[op](...args);
    return Response.json('err' in answer ? answer : {ok:answer.ok??null});
  },
};

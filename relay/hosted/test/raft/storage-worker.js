// The proof uses a deliberately tiny opaque checkpoint store, not P08's
// production member schema. Its awaited transaction demonstrates the actual
// workerd durability boundary before the harness supplies completions.
export class ProofStore {
  constructor(state) { this.state = state; }
  async fetch(request) {
    const key = new URL(request.url).pathname.slice(1);
    if (request.method === 'PUT') {
      const bytes = new Uint8Array(await request.arrayBuffer());
      await this.state.storage.transaction(async tx => { await tx.put(key, bytes); });
      return new Response(null, { status: 204 });
    }
    const bytes = await this.state.storage.get(key);
    return bytes ? new Response(bytes) : new Response(null, { status: 404 });
  }
}

export default {fetch:()=>new Response(null,{status:404})};

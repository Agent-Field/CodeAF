// Durable Object wrapper around MachineRoom (room.js). Kept thin so Node's test runner can exercise MachineRoom
// without importing `cloudflare:workers`.
import { DurableObject } from 'cloudflare:workers';
import { MachineRoom } from './room.js';

export class MachineDO extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.room = new MachineRoom(ctx, env);
  }

  fetch(request) {
    return this.room.fetch(request);
  }

  webSocketMessage(ws, message) {
    return this.room.webSocketMessage(ws, message);
  }

  webSocketClose(ws, code) {
    return this.room.webSocketClose(ws, code);
  }

  webSocketError(ws) {
    return this.room.webSocketError(ws);
  }
}

import { createUsingClient } from './using-client.ts';
import type { UsingApi } from './using-types.ts';

/**
 * The one Using client for the app. `useUsing` re-reads whenever the api it is given changes, so whoever mounts a
 * conversation passes THIS instance (or any other stable one), never a fresh `createUsingClient()` per render.
 */
export const usingApi: UsingApi = createUsingClient();

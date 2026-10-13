import { APP_CONTRACT } from "../../../../docs/design/app-v2/wire";
import type {
  AgentResponse, AppEvent, AppEvents, AppRequest, AppResponse,
  ConflictResponse, CreateAgentRequest, JsonValue, Money,
  PatchAgentRequest, Settings, SetupResponse,
} from "../../../../docs/design/app-v2/wire";

const contract: 1 = APP_CONTRACT;
const create: AppRequest<"POST /app/v1/agents"> = { start: "empty" };
const request: CreateAgentRequest = create;
const reorder: AppRequest<"PUT /app/v1/agents/{id}/queue"> = { order: ["queued-id"] };
declare const inboxItem: AppResponse<"GET /app/v1/inbox/{id}">;
const answerWords: string | undefined = inboxItem.answer?.words;
const receiptText: string | undefined = inboxItem.receipt?.text;
const patch: PatchAgentRequest = { pinned: false, expect: { version: 0 } };
const money: Money = { usd: "4.20" };
const setup: SetupResponse = { step: 1, has_key: false, day_cap: null, complete: false };
const settings: Settings = { "models.default": "model-id", extension: [null, { on: true }] };
const arbitrary: JsonValue = { nested: [null, true, 3, "value"] };
const conflict: ConflictResponse = { error: "changed", request_id: "opaque", reason: "stale", by: "you" };
const hello: AppEvents["hello"] = { epoch: "opaque", codeaf_version: "dev", contract };
const output: AppEvent<"process.output"> = { event: "process.output", data: { process: "p", text: "line" } };

declare const response: AppResponse<"GET /app/v1/agents/{id}">;
const agent: AgentResponse = response;
const name: string = agent.name;
const entries = agent.snapshot.entries;
declare const imageResponse: AppResponse<"GET /app/v1/agents/{id}/image">;
const imageBlob: Blob = imageResponse;

function consume(event: AppEvent): string {
  switch (event.event) {
    case "turn.delta": return event.data.text;
    case "agent.changed": return event.data.agent;
    case "reset": return event.data.reason;
    default: return event.event;
  }
}

// @ts-expect-error Every create request needs its start choice.
const missingStart: CreateAgentRequest = {};
// @ts-expect-error Registered enums reject other app spellings.
const badStart: CreateAgentRequest = { start: "idle" };
// @ts-expect-error Money stays an exact string on the app wire.
const floatMoney: Money = { usd: 4.2 };
// @ts-expect-error An optional scalar update does not encode null.
const nullPinned: PatchAgentRequest = { pinned: null };
// @ts-expect-error A GET route accepts no request body.
const getBody: AppRequest<"GET /app/v1/health"> = {};
// @ts-expect-error Images are binary responses, not JSON strings.
const jsonImage: string = imageResponse;
// @ts-expect-error Unknown routes cannot enter a typed client.
type MissingRoute = AppResponse<"GET /app/v1/not-registered">;
// @ts-expect-error The event name determines its payload.
const wrongPayload: AppEvent<"turn.delta"> = { event: "turn.delta", data: { process: "p", text: "line" } };
// @ts-expect-error Empty JSON-object requests cannot carry UI state.
const signIn: AppRequest<"POST /app/v1/providers/openrouter/sign-in"> = { theme: "dark" };
// @ts-expect-error Embedded Agent fields are flattened, not nested.
const nestedAgent = response.Agent;


import type { FactoryRequest, FactoryResponse } from "../../../../docs/design/app-v2/wire";
const rerun: FactoryRequest<"POST /v1/jobs/{id}/steps/{sid}/rerun"> = { expect: { version: 2 } };
const issue: FactoryRequest<"POST /v1/issues"> = { repo: "org/repo", title: "Fix it", text: "Brief", pipeline: "repair" };
function factoryCard(job: FactoryResponse<"GET /v1/jobs/{id}">): string {
  return job.factory.name + job.state_presentation.title + String(job.controls.stop_step);
}
// @ts-expect-error Factory reads accept no JSON body.
const invalidFactoryRead: FactoryRequest<"GET /v1/jobs/{id}/step-edits"> = {};
// @ts-expect-error Step reruns accept a version number, never a display string.
const invalidStepVersion: FactoryRequest<"POST /v1/jobs/{id}/steps/{sid}/rerun"> = { expect: { version: "2" } };

package api

// FeatureKeyedCreate is the GET /v1/features word of a Factory that keeps a
// keyed job create's answer only once the create has written something. A
// POST /v1/jobs or POST /v1/issues sent with an Idempotency-Key and answered
// before any write carries PhaseHeader: PhasePreMutation and is not kept, so
// the same key runs the create again; an answer given once the job or its
// issue may exist is replayed for 24 hours. An engine sends a create key only
// to a Factory that lists this word: an older one keeps every keyed answer, a
// refusal or a 5xx included, and would replay it for a day.
const FeatureKeyedCreate = "keyed_create"

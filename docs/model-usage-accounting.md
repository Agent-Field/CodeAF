# Model usage accounting

Token usage is collected at the provider attempt boundary for every compatible endpoint, including OpenRouter, Ollama, vLLM, LM Studio, llama.cpp and custom or hosted APIs. The fallback does not depend on a provider name.

## Measurements and estimates

- Connected services request `stream_options.include_usage` on streamed chat completions. An explicit unsupported-field 400 or 422 retries without that option and remembers the refusal for that endpoint. Other validation errors do not disable usage or earn a new repair. The default router road keeps its existing `usage.include` opt-in and sends no `stream_options`.
- Keep provider-reported counts, including explicit zero. Normalize input/output aliases and native cache input without adding cache tokens twice.
- Keep a provider's total-only measurement without inventing its input/output split.
- Estimate only absent components, using `utf8_bytes_4_v1`: round observed UTF-8 bytes up at four bytes per token. Input includes the encoded text messages and tool definitions after shaping; opaque sidecars decline the input estimate. Output includes observed content, each tool name once per call, every argument fragment and visible textual reasoning. Metadata alone observes no output; a normally finished empty answer observes zero.
- Do not estimate media from URL or base64 length. Non-text input stays unknown; output text can still be estimated. Unseen output, encrypted reasoning, hidden thinking and work inside opaque hosted engines cannot be reconstructed.
- Mark interrupted-stream estimates `observed_partial`, never extrapolate to a completed answer. Failed or refused requests without an accepted response do not imply consumption.
- Estimates are approximate, particularly for non-English text, tokenizers and provider chat templates. They are suitable for usage visibility, not billing, token-limit enforcement, benchmark comparisons or exact cost attribution. They never modify response usage or the spend ledger.

## Wire contract

`input_tokens`, `output_tokens` and `total_tokens` remain measurements. `estimated_input_tokens`, `estimated_output_tokens` and `estimated_total_tokens` are separate, optional numeric fields, emitted only for unknown measured components and with a receipt identity. Each total sums only its own present components, except a provider's total-only measurement has no split. Negative counts, counts above 2^53-1 and groups whose sum exceeds that bound remain unknown. Missing is distinct from an explicitly reported or estimated zero. The final status follows the emitted components.

`usage_status` is `reported`, `total_only`, `mixed`, `estimated`, `partial` or `missing`. `estimate_method` and `estimate_scope` label estimates; scopes are `text_only`, `observed_partial`, `multimodal_input` or `unknown`.

`usage_receipt` is a SHA-256 hash scoped to the run and physical attempt. When a generation id exists, its actual endpoint and id participate only locally in that hash alongside the attempt identity. Providers that reuse generation ids cannot collapse separate answers into one receipt. Recovery keeps the original attempt identity. No prompt, response, endpoint, model slug, generation id, API key or other new raw identity is sent. Existing opt-out gates apply to estimates too.

## Recovered receipts and dashboards

An initial estimate and a later recovered provider receipt carry the same `usage_receipt`, but different event UUIDs. Generation ids are trimmed at both boundaries, and blank ids use the attempt identity alone. For each component, prefer a reported count, even if it is zero; retain an estimate only while no measurement exists. One-sided recovery is partial; cost-only recovery adds no token event and keeps its recovered price. A measured total-only receipt supersedes both component estimates. Do not add raw estimates and later measured receipts together.

Native `input_tokens`/`output_tokens` now populate response, call-log and ledger counts where those aliases previously recorded zero. Native cache reads and writes join the prompt count to match canonical cache-inclusive `prompt_tokens`. The provider billing boundary copies any supplied cost and does not price tokens when cost is absent; downstream token pricing sees equivalent native and canonical counts. No estimate is banked, priced or shown as spend. Unmetered direct-service answers, including authorized overflow, retain their prior no-lookup behavior while telemetry identifies the actual attempt.

The existing measured-only series still sums `total_tokens`. The new PostHog view groups by installation, event and receipt hash (falling back to the event UUID for older builds), then selects the best available components. Legacy session-end totals remain separate. Receipt time uses the earliest observation, with a one-day lookback margin around the displayed 14-day range. The query also exposes records with unknown input/output components. This is coverage of recorded receipts, not proof that every model call or anonymous installation reported.

The estimate-aware line chart is configured separately in PostHog. Its estimated series starts only after rollout; historical missing receipts cannot be reconstructed. Verify the actual project receiving `codeaf:usage_delta` before installing a query.

## Deployment order

1. Merge and deploy the companion change in `Agent-Field/website-open-infra`, `site/src/app/api/oss/codeaf/telemetry/`. The previous relay drops unknown status values and strips new properties.
2. Merge the client PR into `dev`, then promote releases normally.
3. Confirm new numeric fields arrive in PostHog. No synthetic events are sent to the production relay by the tests.

The client and relay carry identical `usage-accounting.json` fixtures. Client constructors are checked against that payload and the actual relay handler is checked through a mocked PostHog capture call, including estimates, measured zeroes, partial receipts, privacy stripping, safe integer totals and late-receipt identity. A full 50-event enriched batch is checked against the relay's 64 KiB body cap.

## Receipt-aware PostHog query

```sql
WITH receipts AS (
 SELECT event, distinct_id,
 coalesce(nullIf(toString(properties.usage_receipt), ''), toString(uuid)) AS receipt_key,
 min(timestamp) AS receipt_time,
 max(toFloat(properties.total_tokens)) AS measured_total,
 max(toFloat(properties.input_tokens)) AS measured_input,
 max(toFloat(properties.output_tokens)) AS measured_output,
 max(toFloat(properties.estimated_input_tokens)) AS estimated_input,
 max(toFloat(properties.estimated_output_tokens)) AS estimated_output
 FROM events
 WHERE event IN ('codeaf:usage_delta', 'codeaf:session_ended')
 AND properties.channel='stable' AND coalesce(properties.usage_context,'')!='ci'
 AND timestamp >= today()-INTERVAL 15 DAY
 GROUP BY event, distinct_id, receipt_key
)
SELECT toDate(receipt_time) AS day,
 sumIf(coalesce(measured_total,0), event='codeaf:session_ended') AS legacy_reported_tokens,
 sumIf(if(isNull(measured_input) AND isNull(measured_output),coalesce(measured_total,0),coalesce(measured_input,0)+coalesce(measured_output,0)), event='codeaf:usage_delta') AS reported_tokens,
 sumIf(if(isNotNull(measured_total) AND isNull(measured_input) AND isNull(measured_output),0,if(isNull(measured_input),coalesce(estimated_input,0),0)+if(isNull(measured_output),coalesce(estimated_output,0),0)),event='codeaf:usage_delta') AS estimated_tokens,
 countIf(event='codeaf:usage_delta' AND ((isNull(measured_input) AND isNull(estimated_input)) OR (isNull(measured_output) AND isNull(estimated_output)))) AS records_with_unknown_components
FROM receipts WHERE receipt_time >= today()-INTERVAL 14 DAY
GROUP BY day ORDER BY day
```


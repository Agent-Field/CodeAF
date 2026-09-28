# Critical: Hermes doesn't validate finish_reason for tool_calls, allowing truncated JSON to be processed

## 🐛 Bug Report

### Summary
Hermes does not validate `finish_reason` when `tool_calls` are present, allowing truncated/incomplete JSON in tool call arguments to be processed. This causes cascading errors including HTTP 500 responses and wasted retry attempts.

### Impact
- **Severity:** Critical
- **Affects:** All users using models with output token limits
- **Symptoms:** HTTP 500 errors, invalid JSON in tool calls, session failures

### Root Cause

When a model returns `finish_reason="length"` with `tool_calls` present, the current code only checks for truncation when tool_calls are **absent** (`run_agent.py:7803`):

```python
if finish_reason == "length":
    if self.api_mode == "chat_completions":
        assistant_message = response.choices[0].message
        if not assistant_message.tool_calls:  # ← Only handles missing tool_calls
            # continuation retry logic
```

**Problem:** When `tool_calls` exist, the code skips the length check entirely and proceeds to process potentially incomplete JSON arguments.

### Reproduction

1. Use a model with limited output tokens (e.g., `max_tokens=4096`)
2. Request a task that generates a large tool call (e.g., `write_file` with substantial content)
3. Model hits token limit mid-generation
4. Returns `finish_reason="length"` (or router rewrites to `"tool_calls"`) with incomplete JSON:
   ```json
   {"path": "/tmp/file.md", "content": "very long text...
   ```
5. Hermes processes the broken JSON → validation fails → 3 retries → tool error → HTTP 500

### Example from Real Session

**Session:** `20260411_121436_65708048`

**Truncated tool call:**
```json
{"path": "/tmp/ai_gateway_comparison_report.md"
```
(Missing closing brace and `content` parameter)

**Result:**
- 3 retry attempts (all failed)
- HTTP 500 from router
- Session terminated

**Context stats:**
- 48K tokens (24% of 200K limit) - context was healthy
- 45 tool calls (44 successful, 1 truncated)
- No compression needed

### Proposed Fix

Add validation for truncated tool_calls when `finish_reason="length"`:

```python
if finish_reason == "length":
    if self.api_mode == "chat_completions":
        assistant_message = response.choices[0].message
        
        # NEW: Validate tool_calls JSON completeness
        if assistant_message.tool_calls:
            incomplete_tools = []
            for tc in assistant_message.tool_calls:
                args = tc.function.arguments or ""
                if not args.strip():
                    continue
                try:
                    json.loads(args)
                except json.JSONDecodeError as e:
                    incomplete_tools.append((tc.function.name, str(e)))
            
            if incomplete_tools:
                tool_name, error = incomplete_tools[0]
                # Return user-friendly error instead of processing broken JSON
                return {
                    "final_response": "⚠️ Tool call truncated due to length limit",
                    "error": f"Tool call truncated: {tool_name} - {error}",
                    "partial": True
                }
        
        # Existing logic for missing tool_calls
        if not assistant_message.tool_calls:
            # continuation retry
```

### Benefits

1. **Prevents cascading errors** - catches truncation early
2. **Saves API calls** - no wasted retries on unfixable truncation
3. **Better UX** - clear error message with actionable suggestions
4. **Preserves context** - doesn't pollute message history with broken JSON

### Testing

Tested fix locally - successfully catches truncated tool calls and returns helpful error instead of HTTP 500.

### Files Affected

- `run_agent.py` (lines 7801-7803)

### Related Code

The existing invalid JSON retry logic (lines 8845-8883) doesn't distinguish between:
- Recoverable JSON errors (model mistake) → retry makes sense
- Truncation errors (length limit) → retry is pointless

This fix addresses the root cause before broken JSON enters the retry loop.

---

**Environment:**
- Hermes version: latest (2026-04-11)
- Model: kr/claude-sonnet-4.5
- Router: router.neomentor.tech
- Platform: Matrix

**Analysis:** Full detailed analysis available in session logs.

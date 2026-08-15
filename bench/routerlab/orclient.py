#!/usr/bin/env python3
"""Minimal async OpenRouter client shared by the smoke test and the runner.

Everything the experiment needs from a call is returned in one record: the
text, the token counts, the computed cost, and the wall latency. Cost is
computed from panel.json prices rather than read back from the API so that a
cell's cost is reproducible from the saved panel even if pricing later moves.
"""
import asyncio
import json
import os
import random
import time

import httpx

BASE_URL = os.environ.get("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
TIMEOUT_S = 120.0
MAX_TOKENS = 4096
TEMPERATURE = 0.2

# Retry on transport hiccups and provider-side congestion only. A 400/404 is a
# fact about the request, not a hiccup, and retrying it just burns wall clock.
RETRY_STATUS = {408, 409, 429, 500, 502, 503, 504, 520, 522, 524}
MAX_ATTEMPTS = 5


class Usage:
    __slots__ = ("prompt", "completion", "reasoning", "cost")

    def __init__(self, prompt=0, completion=0, reasoning=0, cost=0.0):
        self.prompt, self.completion = prompt, completion
        self.reasoning, self.cost = reasoning, cost


def _headers():
    key = os.environ.get("OPENROUTER_API_KEY", "")
    if not key:
        raise SystemExit("OPENROUTER_API_KEY is not set")
    return {
        "Authorization": f"Bearer {key}",
        "Content-Type": "application/json",
        "HTTP-Referer": "https://github.com/Agent-Field/aforge-v2",
        "X-Title": "aforge routerlab",
    }


async def chat(client, model, messages, *, price_in=0.0, price_out=0.0,
               json_mode=False, max_tokens=MAX_TOKENS, reasoning_off=True):
    """One chat completion. Returns (text, Usage, latency_s, error_or_None)."""
    body = {
        "model": model,
        "messages": messages,
        "temperature": TEMPERATURE,
        "max_tokens": max_tokens,
    }
    if json_mode:
        body["response_format"] = {"type": "json_object"}
    if reasoning_off:
        # aforge runs with reasoning off in production (see config.go); the
        # panel is measured the way the harness would actually call it.
        body["reasoning"] = {"enabled": False}

    last_err = None
    t0 = time.monotonic()
    for attempt in range(MAX_ATTEMPTS):
        try:
            r = await client.post(f"{BASE_URL}/chat/completions",
                                  headers=_headers(), json=body,
                                  timeout=TIMEOUT_S)
            if r.status_code in RETRY_STATUS:
                last_err = f"http {r.status_code}: {r.text[:200]}"
                await asyncio.sleep(min(2 ** attempt, 16) + random.random() * 2)
                continue
            if r.status_code != 200:
                return "", Usage(), time.monotonic() - t0, \
                    f"http {r.status_code}: {r.text[:300]}"
            data = r.json()
        except Exception as e:  # transport / timeout / decode
            last_err = f"{type(e).__name__}: {e}"
            await asyncio.sleep(min(2 ** attempt, 16) + random.random() * 2)
            continue

        if "error" in data and not data.get("choices"):
            return "", Usage(), time.monotonic() - t0, \
                f"api error: {json.dumps(data['error'])[:300]}"
        choices = data.get("choices") or []
        if not choices:
            return "", Usage(), time.monotonic() - t0, "no choices in response"
        msg = choices[0].get("message") or {}
        text = msg.get("content") or ""
        u = data.get("usage") or {}
        pt = int(u.get("prompt_tokens") or 0)
        ct = int(u.get("completion_tokens") or 0)
        rt = 0
        det = u.get("completion_tokens_details") or {}
        if isinstance(det, dict):
            rt = int(det.get("reasoning_tokens") or 0)
        cost = pt / 1e6 * price_in + ct / 1e6 * price_out
        return text, Usage(pt, ct, rt, cost), time.monotonic() - t0, None

    return "", Usage(), time.monotonic() - t0, f"exhausted retries: {last_err}"


def new_client():
    limits = httpx.Limits(max_connections=64, max_keepalive_connections=32)
    return httpx.AsyncClient(limits=limits, timeout=TIMEOUT_S)

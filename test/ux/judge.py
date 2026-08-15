#!/usr/bin/env python3
"""judge.py — one model call that grades one deliverable.

A journey can prove that a haiku was journaled, spliced, and shown. It cannot
prove the haiku was any good, and "it ran" is not the product's promise. So the
deliverables get read by a cheap model against a three-line rubric, and the
score rides in the report beside the pass.

    judge.py <state-dir> <ask-file> <deliverable-file>

prints:  <score 1-5>|<one-line justification>
"""

import json
import os
import sys
import urllib.error
import urllib.request

RUBRIC = """You are grading one deliverable from an AI agent. Be a hard marker.

Grade on exactly three things:
1. Does the deliverable satisfy the LITERAL ask — every constraint the user
   actually wrote (counts, subjects, named requirements), not the gist?
2. Is it answer-first: does the final message contain the CONTENT itself,
   rather than only a pointer to a file the user would have to go and open?
3. Is it competently done for what it is?

Reply with JSON only: {"score": <1-5 integer>, "why": "<one short sentence>"}
5 = satisfies every literal constraint and leads with the content.
3 = broadly right but misses a stated constraint or buries the content.
1 = does not do what was asked, or hands back only a path."""


def main() -> int:
    state, ask_path, deliverable_path = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(os.path.join(state, "config.json")) as handle:
        key = json.load(handle)["api_key"]
    # The "~" prefix is OpenRouter's floating-alias marker and part of the slug,
    # not decoration: stripping it turns a valid model id into a 400. The
    # fallbacks exist because the judge failing must never fail the journey.
    try:
        configured = json.load(open(os.path.join(state, "settings.json"))).get("chat_model")
    except (FileNotFoundError, ValueError):
        configured = None
    candidates = [m for m in (configured, "deepseek/deepseek-v4-flash",
                              "google/gemini-3.6-flash") if m]

    ask = open(ask_path).read().strip()[:2000]
    deliverable = open(deliverable_path).read().strip()[:6000]
    if not deliverable:
        print("1|nothing was delivered to grade")
        return 0

    text, failure = None, "no model answered"
    for model in candidates:
        payload = {
            "model": model,
            "messages": [
                {"role": "system", "content": RUBRIC},
                {"role": "user", "content": "THE ASK:\n%s\n\nWHAT CAME BACK:\n%s" % (ask, deliverable)},
            ],
            "temperature": 0,
            # Generous, because a reasoning-capable model spends its first
            # tokens thinking and returns a null content when the cap lands
            # mid-thought — which reads as "the judge said nothing" rather than
            # as the truncation it is.
            "max_tokens": 900,
        }
        request = urllib.request.Request(
            "https://openrouter.ai/api/v1/chat/completions",
            data=json.dumps(payload).encode(),
            headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                body = json.load(response)
            message = body["choices"][0]["message"]
            text = message.get("content") or message.get("reasoning")
            if text:
                break
            failure = "%s returned no content (%s)" % (
                model, body["choices"][0].get("finish_reason"))
        except (urllib.error.URLError, KeyError, ValueError, TypeError) as exc:
            failure = "%s on %s" % (exc, model)
    if text is None:
        print("?|the judge call failed: %s" % failure)
        return 0

    start, end = text.find("{"), text.rfind("}")
    if start >= 0 and end > start:
        try:
            verdict = json.loads(text[start:end + 1])
            why = str(verdict.get("why", "")).replace("|", "/").replace("\n", " ")
            print("%s|%s" % (verdict.get("score", "?"), why[:200]))
            return 0
        except ValueError:
            pass
    print("?|the judge did not answer in the rubric's shape: %s" % text[:120].replace("\n", " "))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

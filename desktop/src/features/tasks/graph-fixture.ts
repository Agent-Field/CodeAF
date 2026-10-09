import type { PlanDependency, PlanTaskRow, PlanTaskPage } from './plan-model';

/**
 * Read-only capture of a completed canonical plan, including the verification
 * tasks created by its workers. Titles are shortened for a public UI preview;
 * task IDs, containment, dependency kinds, and captured states are preserved.
 * This fixture never starts work and is not a live engine projection.
 */
export const researchGraphFixture = {
 title: 'Acquisition research',
 sourceLabel: 'Read-only captured plan · completed research',
 capturedAt: "2026-10-08T22:11:38.276596Z",
 description: '19 completed tasks, including 9 worker verification tasks. Eight recorded feeds-into relationships connect research to synthesis; containment groups the work under its research objective. No live work is running in this preview.',
 rows: [
  {
    "ID": "1",
    "Title": "DataRobot acquisition research",
    "Status": "done"
  },
  {
    "ID": "2",
    "Title": "AgentField company and team",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "3",
    "Title": "Agnostiq and Covalent history",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "4",
    "Title": "AgentField technical research",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "prdhh6",
    "Title": "Acquisition and interpretation",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "jtgxiv",
    "Title": "Covalent under DataRobot",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "uvtgsi",
    "Title": "DataRobot agentic strategy",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "u5x46p",
    "Title": "Agent infrastructure competitors",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "jqtucd",
    "Title": "Research note synthesis",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "6",
    "Title": "Final research report",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "jsnhhj",
    "Title": "Verify acquisition research",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "wkzb1j",
    "Title": "Verify agentic strategy",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "paac6t",
    "Title": "Verify Agnostiq and Covalent",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "zk0ghl",
    "Title": "Verify company and team",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "8jyp9j",
    "Title": "Verify technical research",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "wrvbhg",
    "Title": "Verify competitor research",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "qe725x",
    "Title": "Verify Covalent research",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "9e2nb4",
    "Title": "Verify research synthesis",
    "Status": "done",
    "Parent": "1"
  },
  {
    "ID": "uxkmb2",
    "Title": "Verify final report",
    "Status": "done",
    "Parent": "1"
  }
] satisfies PlanTaskRow[],
 dependencies: [
  {
    "from": "2",
    "to": "6",
    "kind": "feeds_into"
  },
  {
    "from": "3",
    "to": "6",
    "kind": "feeds_into"
  },
  {
    "from": "4",
    "to": "6",
    "kind": "feeds_into"
  },
  {
    "from": "jqtucd",
    "to": "6",
    "kind": "feeds_into"
  },
  {
    "from": "prdhh6",
    "to": "jqtucd",
    "kind": "feeds_into"
  },
  {
    "from": "jtgxiv",
    "to": "jqtucd",
    "kind": "feeds_into"
  },
  {
    "from": "uvtgsi",
    "to": "jqtucd",
    "kind": "feeds_into"
  },
  {
    "from": "u5x46p",
    "to": "jqtucd",
    "kind": "feeds_into"
  }
] satisfies PlanDependency[],
};

/** Actual archived canonical plan; only display titles are sanitized. */
export const graphFixture = {
 title: "Source ingestion",
 sourceLabel: "Read-only captured plan \u00b7 source ingestion",
 capturedAt: "2026-10-08T22:23:15.221588Z",
 description: "19 captured tasks across four hierarchy levels: 12 completed and 7 cancelled. Five recorded feeds-into relationships join source work to verification. Cancelled nested work remains visible exactly as captured; this preview starts no work.",
 rows: [
  {
    "ID": "root",
    "Title": "Source ingestion",
    "Status": "done"
  },
  {
    "ID": "94pzii",
    "Title": "Email sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "stwczi",
    "Title": "Question-and-answer sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "6qmw4i",
    "Title": "Community discussion sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "5g2v2z",
    "Title": "Product review sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "yy9bhh",
    "Title": "Policy and moderation sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "7bs0sp",
    "Title": "Verify source formats and tests",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "58utnd",
    "Title": "Email routing and urgency",
    "Status": "cancelled",
    "Parent": "yy9bhh"
  },
  {
    "ID": "nsuski",
    "Title": "Forum question sources",
    "Status": "cancelled",
    "Parent": "yy9bhh"
  },
  {
    "ID": "6j8lzc",
    "Title": "Review ratings and aspects",
    "Status": "cancelled",
    "Parent": "yy9bhh"
  },
  {
    "ID": "z8okzz",
    "Title": "Find email candidates",
    "Status": "cancelled",
    "Parent": "58utnd"
  },
  {
    "ID": "mdhvuh",
    "Title": "Find question-and-answer candidates",
    "Status": "cancelled",
    "Parent": "58utnd"
  },
  {
    "ID": "12is1c",
    "Title": "Find product review candidates",
    "Status": "cancelled",
    "Parent": "58utnd"
  },
  {
    "ID": "a36req",
    "Title": "Find moderation candidates",
    "Status": "cancelled",
    "Parent": "58utnd"
  },
  {
    "ID": "lf03hw",
    "Title": "Verify product reviews",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "wxw5zy",
    "Title": "Verify email sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "is952j",
    "Title": "Verify community discussions",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "ufrbf3",
    "Title": "Verify question-and-answer sources",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "ssg0hf",
    "Title": "Verify source tests",
    "Status": "done",
    "Parent": "root"
  }
] satisfies PlanTaskRow[],
 dependencies: [
  {
    "from": "94pzii",
    "to": "7bs0sp",
    "kind": "feeds_into"
  },
  {
    "from": "stwczi",
    "to": "7bs0sp",
    "kind": "feeds_into"
  },
  {
    "from": "6qmw4i",
    "to": "7bs0sp",
    "kind": "feeds_into"
  },
  {
    "from": "5g2v2z",
    "to": "7bs0sp",
    "kind": "feeds_into"
  },
  {
    "from": "yy9bhh",
    "to": "7bs0sp",
    "kind": "feeds_into"
  }
] satisfies PlanDependency[],
};

/** Actual archived canonical plan; only display titles are sanitized. */
export const comparisonGraphFixture = {
 title: "Model evaluation",
 sourceLabel: "Read-only captured plan \u00b7 model evaluation",
 capturedAt: "2026-10-08T22:23:15.221588Z",
 description: "18 completed captured tasks across three hierarchy levels. Six recorded feeds-into and two blocking relationships preserve the original evaluation plan. This preview starts no work.",
 rows: [
  {
    "ID": "root",
    "Title": "Model evaluation",
    "Status": "done"
  },
  {
    "ID": "ee0bu0",
    "Title": "Teacher client and tests",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "7asux2",
    "Title": "Stratified evaluation subset",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "bksp67",
    "Title": "Local scorer and throughput",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "tiqpbj",
    "Title": "Model probes",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "g3laat",
    "Title": "Evaluation runner and report",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "a6qwcw",
    "Title": "Check teacher contract",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "g97d58",
    "Title": "Check evaluation measurements",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "oou6fv",
    "Title": "Verify evaluation subset",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "w0ihyv",
    "Title": "Verify teacher client",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "6xsqpj",
    "Title": "Verify contract checks",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "ejh0fs",
    "Title": "Verify local scoring",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "2akaz6",
    "Title": "Verify model probes",
    "Status": "done",
    "Parent": "root"
  },
  {
    "ID": "62evg9",
    "Title": "Evaluate response models",
    "Status": "done",
    "Parent": "g3laat"
  },
  {
    "ID": "unrky8",
    "Title": "Evaluate routing model",
    "Status": "done",
    "Parent": "g3laat"
  },
  {
    "ID": "s76cf7",
    "Title": "Verify routing evaluation",
    "Status": "done",
    "Parent": "g3laat"
  },
  {
    "ID": "zvdd5n",
    "Title": "Verify response evaluation",
    "Status": "done",
    "Parent": "g3laat"
  },
  {
    "ID": "ori6rz",
    "Title": "Verify evaluation measurements",
    "Status": "done",
    "Parent": "root"
  }
] satisfies PlanTaskRow[],
 dependencies: [
  {
    "from": "ee0bu0",
    "to": "a6qwcw",
    "kind": "blocks"
  },
  {
    "from": "7asux2",
    "to": "bksp67",
    "kind": "feeds_into"
  },
  {
    "from": "ee0bu0",
    "to": "g3laat",
    "kind": "feeds_into"
  },
  {
    "from": "7asux2",
    "to": "g3laat",
    "kind": "feeds_into"
  },
  {
    "from": "bksp67",
    "to": "g3laat",
    "kind": "feeds_into"
  },
  {
    "from": "tiqpbj",
    "to": "g3laat",
    "kind": "feeds_into"
  },
  {
    "from": "g3laat",
    "to": "g97d58",
    "kind": "blocks"
  },
  {
    "from": "7asux2",
    "to": "tiqpbj",
    "kind": "feeds_into"
  }
] satisfies PlanDependency[],
};

/** Actual task-record excerpts, not reconstructed chat or live assistant turns. */
export const capturedTaskPages: Record<string, PlanTaskPage> = {
  "root": {
    "Row": {
      "ID": "root",
      "Title": "Source ingestion",
      "Status": "done"
    },
    "Description": "Every source: map to {text, schema{head: labels(+descriptions)}}; dedup against fast-decisions dev (common dedup guard); keep gold/outcome labels as extra field where they exist; namespace ids by source; write NEW shards only (never rewrite live shards); card with counts per head type; 5-50k texts per source first (bake-off size), more on demand. Enron (folder routing, reply?, reply-latency urgency outcome), StackExchange/Reddit (site/tag, answered/accepted), reviews (stars ordinal, category path, aspects), ToS/moderation (policy category).",
    "Result": "LANE [source group] integrated and closed. All 5 source tasks + 1 verifier + 5 check seats landed with no open descendants."
  },
  "94pzii": {
    "Row": {
      "ID": "94pzii",
      "Title": "Email sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: decide/wave/src_enron.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_enron --limit N`, plus decide/tests/test_wave_enron.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: Enron email with folder metadata (candidates corbt/enron-emails, aeslc, SetFit/enron_spam, CMU maildir mirrors). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream with datasets.load_dataset(streaming=True) or hf_hub_download; never load a whole corpus. Output: NEW shards only, ShardWriter(\"enron\") into [redacted path] (OUTSIDE\u2026",
    "Result": "Enron lane done (NO git commit: my work order says do not commit, overriding the lane rule)."
  },
  "stwczi": {
    "Row": {
      "ID": "stwczi",
      "Title": "Question-and-answer sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: decide/wave/src_stackexchange.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_stackexchange --limit N`, plus decide/tests/test_wave_stackexchange.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: StackExchange questions/answers (candidates mikex86/stackoverflow-posts, HuggingFaceH4/stack-exchange-preferences, per-site dumps). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWriter(\"stackexchange\") into [redacted path] (OUTSIDE the git wo\u2026",
    "Result": "StackExchange forum lane done. FILES: decide/wave/src_stackexchange.py (build(limit_texts,out_source)->card, CLI --limit/--out, raw_heads(record), memory guard via free -g >=15GB, reset of only its own source dir), decide/tests/test_wave_stackexchange.py (15 offline tests, 1.6 s, tiny fixtures + one skip-guarded integrity test over the produced shards). ARTIFACT: [redacted path] + CARD.json = 20000 texts / 20000 pairs, dup_dropped 0, schema_dropped 0, rows_used 12000 (mikex86/stackoverflow-posts) + 8000 (HuggingFaceH4/stack-exchange-preferences). LICENCES: mikex86/stackoverflow-posts card=\"other\" but every row carries ContentLicense CC BY-SA 2.5/3.0/4.0 -> non-CC-BY-SA rows filtered, nothing skipped for a training-forbidding licence; stack-exchange-preferences = cc-by-sa-4.0. COUNTS PER HE\u2026"
  },
  "6qmw4i": {
    "Row": {
      "ID": "6qmw4i",
      "Title": "Community discussion sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: decide/wave/src_reddit.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_reddit --limit N`, plus decide/tests/test_wave_reddit.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: Reddit posts with subreddit/comment metadata (candidates webis/tldr-17, social_bias_frames, pushshift-derived small subsets). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWriter(\"reddit\") into [redacted path] (OUTSIDE the git worktree), default ~20000 texts. On \u2026",
    "Result": "Files: decide/wave/src_reddit.py (build(limit_texts, out_source)->card; CLI --limit/--out-source/--datasets; raw_heads(record); licence gate; fresh_dir; free -g>=15 abort) and decide/tests/test_wave_reddit.py (14 offline tests). Shards (NEW): [redacted path] pairs-00000.jsonl, CARD.json} = 20000 texts / 20000 pairs. Counts per head type: routing 20000, topic 20000, outcome 52000. Heads: subreddit (routing, gold 20000, label sets vary 6-24 in shuffled order), topic_tags (topic, multi_label, gold 10419), will_get_reply + engagement_level (outcome, gold 16000 each, bucketed from num_comments), post_score_band (outcome, gold 20000 from score); 5 heads/text for the 16000 Pushshift rows, 3 for the 4000 CSV rows. Licences: fddemarco/pushshift-reddit = no licence field on the card (unresolved, Red\u2026"
  },
  "5g2v2z": {
    "Row": {
      "ID": "5g2v2z",
      "Title": "Product review sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: decide/wave/src_reviews.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_reviews --limit N`, plus decide/tests/test_wave_reviews.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: product reviews with stars and category metadata (candidates McAuley-Lab/Amazon-Reviews-2023, SetFit/amazon_reviews_multi, fancyzhx/amazon_polarity; skip non-commercial-only licences such as yelp_review_full). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWrit\u2026",
    "Result": "Files: decide/wave/src_reviews.py (build(limit_texts, out_source)->card, CLI `python -m decide.wave.src_reviews --limit N`, raw_heads(record), _algebra hook calling decide.wave.algebra.derive_all(..., k=3) when present) and decide/tests/test_wave_reviews.py (12 offline tests, 0.83 s). Acceptance: `python -m pytest -q decide/tests/test_wave_*.py` -> 66 passed, 1 warning in 3.23s, exit 0 (my file alone: 12 passed, exit 0); real CPU-only run `python -m decide.wave.src_reviews --limit 20000` exit 0, gated by require_memory() on `free -g` (available 70 GB now; aborts below 15 GB). Output (shared, outside git; worktree data_out is a sibling-created symlink to the same root, inode match 31360938): [redacted path] 10.3MB, pairs-00000.jsonl 71.5MB, CARD.json}. CARD.json counts: texts=20000, pairs=2\u2026"
  },
  "yy9bhh": {
    "Row": {
      "ID": "yy9bhh",
      "Title": "Policy and moderation sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: decide/wave/src_moderation.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_moderation --limit N`, plus decide/tests/test_wave_moderation.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: ToS/moderation comment sets (candidates google/civil_comments, thesofakillers/jigsaw-toxic-comment-classification-challenge, mmathys/openai-moderation-api-evaluation, allenai/real-toxicity-prompts). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWriter\u2026",
    "Result": "Moderation lane ([source group]) delivered."
  },
  "7bs0sp": {
    "Row": {
      "ID": "7bs0sp",
      "Title": "Verify source formats and tests",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Goal: independent verification of the whole [source group] wave (all five source dirs) plus decide/tests/test_wave_integrity.py, an offline fast (under 60 s) integrity test over the produced shards. Inputs: the five modules decide/wave/src_{enron,stackexchange,reddit,reviews,moderation}.py and their CARD.json at [redacted path] Prove, sentence by sentence: (1) the acceptance command passes; (2) every texts-*.jsonl has a tid of the form <source>-t<NNN>-<NNNNN> and every pairs-*.jsonl entry has a matching tid and a pid <tid>:<k>; (3) no written text duplicates a fast-decisions text (rebuild decide.corpus.dedup.EvalGuard over the eval texts and re-check, streaming the shards); (4) every head name passes decide.common.schema_ok and every head has 2..255 cleaned labels with no reserved marker t\u2026",
    "Result": "Independent offline verification of the whole [source group] wave: wrote decide/tests/test_wave_integrity.py (38 tests, no network/GPU, skips cleanly on a missing source dir). ACCEPTANCE `cd [redacted path] && HF_HOME=[redacted path] [redacted path] -m pytest -q decide/tests/test_wave_integrity.py` -> exit 0, \"38 passed, 1 warning in 35.99s\". All seven points hold against the real shards: (1) command passes; (2) 99104 tids match <source>-t<NNN>-<NNNNN> with NNN == the shard file index and are unique, every pairs entry has a matching tid and pid \"<tid>:<k>\" dense from 0; (3) EvalGuard rebuilt over all 1700 fast-decisions texts and streamed over all 99104 shard texts found 0 duplicates; (4) every head passes schema_ok and carries 2..254 cleaned labels with no reserved markers/parentheses; (5\u2026"
  },
  "58utnd": {
    "Row": {
      "ID": "58utnd",
      "Title": "Email routing and urgency",
      "Status": "cancelled",
      "Parent": "yy9bhh"
    },
    "Description": "Source: Enron email. Heads: folder routing (gold = maildir folder), needs-a-reply / reply expected, urgency from reply latency where derivable.\nDeliverable: decide/wave/src_NAME.py with build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_NAME --limit N`; plus decide/tests/test_wave_NAME.py (offline, tiny fixtures, <60s, no network).\nRead FIRST and do NOT edit: decide/common.py, decide/wave/common.py (ShardWriter, head_spec, clean_schema, dev_guard), decide/wave/srcutil.py (clean_text, emit). Do not edit any other lane file.\nPython: [redacted path] with HF_HOME=[redacted path] CPU only; run `free -g` first and abort if available < 15 GB. Stream datasets (load_dataset(..., streaming=True)); never load a whole corpus into RAM. Do not pip install. Do not run python from /\u2026"
  },
  "nsuski": {
    "Row": {
      "ID": "nsuski",
      "Title": "Forum question sources",
      "Status": "cancelled",
      "Parent": "yy9bhh"
    },
    "Description": "Source: StackExchange / Reddit. Heads: which site / tag (gold = site or tag), will it be answered (gold = has answers), will it be accepted (gold = accepted answer) where the dataset carries it.\nDeliverable: decide/wave/src_NAME.py with build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_NAME --limit N`; plus decide/tests/test_wave_NAME.py (offline, tiny fixtures, <60s, no network).\nRead FIRST and do NOT edit: decide/common.py, decide/wave/common.py (ShardWriter, head_spec, clean_schema, dev_guard), decide/wave/srcutil.py (clean_text, emit). Do not edit any other lane file.\nPython: [redacted path] with HF_HOME=[redacted path] CPU only; run `free -g` first and abort if available < 15 GB. Stream datasets (load_dataset(..., streaming=True)); never load a whole corpus int\u2026"
  },
  "6j8lzc": {
    "Row": {
      "ID": "6j8lzc",
      "Title": "Review ratings and aspects",
      "Status": "cancelled",
      "Parent": "yy9bhh"
    },
    "Description": "Source: product reviews. Heads: stars as an ORDINAL head (1..5 wording like 'one star'/'five stars'), category path (gold = product category), aspects (gold = aspect terms where present), helpfulness where present.\nDeliverable: decide/wave/src_NAME.py with build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_NAME --limit N`; plus decide/tests/test_wave_NAME.py (offline, tiny fixtures, <60s, no network).\nRead FIRST and do NOT edit: decide/common.py, decide/wave/common.py (ShardWriter, head_spec, clean_schema, dev_guard), decide/wave/srcutil.py (clean_text, emit). Do not edit any other lane file.\nPython: [redacted path] with HF_HOME=[redacted path] CPU only; run `free -g` first and abort if available < 15 GB. Stream datasets (load_dataset(..., streaming=True)); never loa\u2026"
  },
  "z8okzz": {
    "Row": {
      "ID": "z8okzz",
      "Title": "Find email candidates",
      "Status": "cancelled",
      "Parent": "58utnd"
    },
    "Description": "Goal: for lane [source group] (email family) establish exactly how to stream the best Enron dataset into decide/wave/src_misc.py. Candidates in HF cache ([redacted path]): corbt/enron-emails, SetFit/enron_spam, LLM-PBE/enron-email, snoop2head/enron_aeslc_emails, amanneo/enron-mail-corpus-mini, enronarchive/mail, Hellisotherpeople/enron_emails_parsed, SnowZeng/enron_mail, suolyer/pile_enron. Bounded streaming probes only (datasets.load_dataset(..., streaming=True), take(3)); CPU only; HF_HOME=[redacted path] use [redacted path] never run python from /tmp; no pip install; never load a whole corpus. Owned output: [redacted path] - a table, one row per dataset: id | licence EXACTLY as the card states (quote it; if the card has no licence field say so and where you looked) | snapshot dir path u\u2026"
  },
  "mdhvuh": {
    "Row": {
      "ID": "mdhvuh",
      "Title": "Find question-and-answer candidates",
      "Status": "cancelled",
      "Parent": "58utnd"
    },
    "Description": "Goal: for lane [source group] (forums family) establish exactly how to stream a Q&A dataset with site/tag + answered/accepted gold into decide/wave/src_misc.py. Candidates in HF cache ([redacted path]): flax-sentence-embeddings/stackexchange_title_body_jsonl, HuggingFaceH4/stack-exchange-preferences, mikex86/stackoverflow-posts, fddemarco/pushshift-reddit. Also search the Hub with huggingface_hub HfApi().list_datasets(search=...) for 2-4 more candidates carrying tags / accepted-answer / score fields. Bounded streaming probes only (streaming=True, take(3)); CPU only; HF_HOME=[redacted path] [redacted path] never from /tmp; no pip install; never load a whole corpus; do not download more than ~500MB. Owned output: [redacted path] - table one row per dataset: id | licence EXACTLY as the card s\u2026"
  },
  "12is1c": {
    "Row": {
      "ID": "12is1c",
      "Title": "Find product review candidates",
      "Status": "cancelled",
      "Parent": "58utnd"
    },
    "Description": "Goal: for lane [source group] (reviews family) establish exactly how to stream a product-review dataset with ordinal star gold, category path and aspects into decide/wave/src_misc.py. Candidates in HF cache ([redacted path]): amazon_reviews_multi, goosmanlei/amazon_reviews_multi, mteb/amazon_reviews_multi, McAuley-Lab/Amazon-Reviews-2023, milistu/AMAZON-Products-2023, Studeni/AMAZON-Products-2023, Jyshen/amazon_review_metadata, ruhailshaikh/Amazon_Review_Data, ckandemir/amazon-products, Yelp/yelp_review_full, SetFit/yelp_review_full, yelp_review_full, fancyzhx/yelp_polarity, cornell-movie-review-data/rotten_tomatoes. Bounded streaming probes only (streaming=True, take(3)); CPU only; HF_HOME=[redacted path] [redacted path] never from /tmp; no pip install; if a dataset is not cached do NOT d\u2026"
  },
  "a36req": {
    "Row": {
      "ID": "a36req",
      "Title": "Find moderation candidates",
      "Status": "cancelled",
      "Parent": "58utnd"
    },
    "Description": "Goal: for lane [source group] (moderation family) establish exactly how to stream a ToS/moderation dataset with a policy-category gold label into decide/wave/src_misc.py. Candidates in HF cache ([redacted path]): ayanmaj/ModerationBench-4K, mmathys/openai-moderation-api-evaluation, thesofakillers/jigsaw-toxic-comment-classification-challenge, mteb/toxic_conversations_50k, allenai/real-toxicity-prompts. Also HfApi().list_datasets(search='moderation') for 2-4 more with policy categories. Bounded streaming probes only (streaming=True, take(3)); CPU only; HF_HOME=[redacted path] [redacted path] never from /tmp; no pip install; never load a whole corpus. Owned output: [redacted path] - table one row per dataset: id | licence EXACTLY as the card states (quote; say if absent) | cached snapshot di\u2026"
  },
  "lf03hw": {
    "Row": {
      "ID": "lf03hw",
      "Title": "Verify product reviews",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Acceptance: Goal: decide/wave/src_reviews.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_reviews --limit N`, plus decide/tests/test_wave_reviews.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: product reviews with stars and category metadata (candidates McAuley-Lab/Amazon-Reviews-2023, SetFit/amazon_reviews_multi, fancyzhx/amazon_polarity; skip non-commercial-only licences such as yelp_review_full). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards onl\u2026",
    "Result": "check: both declared commands were run and both exited 0 \u2014 pytest -q decide/tests/test_wave_reviews.py (12 passed) and test -s [redacted path] \u2014 and every acceptance sentence probed true (build(limit_texts,out_source)->card with raw_heads and CLI --limit present; NEW shards only outside the git worktree; texts-00000.jsonl 20000 texts, pairs-00000.jsonl 20000 pairs; heads 2-6 with head_types rating/routing/aspect; labels 2-255; gold a subset of labels; text max 1711 chars; require_memory aborts below the 15 GB gate). A \"holds:\" verdict cannot be recorded: the first declared check is stored as \"cd [redacted path] && HF_HOME=... pytest ...\", a shell composition that the store audit law (auditableDeclaredCheck via approval.ShellComposition) refuses to certify for any run, and plandb task set-c\u2026"
  },
  "wxw5zy": {
    "Row": {
      "ID": "wxw5zy",
      "Title": "Verify email sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Acceptance: Goal: decide/wave/src_enron.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_enron --limit N`, plus decide/tests/test_wave_enron.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: Enron email with folder metadata (candidates corbt/enron-emails, aeslc, SetFit/enron_spam, CMU maildir mirrors). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream with datasets.load_dataset(streaming=True) or hf_hub_download; never load a whole corpus. Output: NEW shards only, ShardWriter(\"enron\") into [redacted pa\u2026",
    "Result": "check: both declared checks run verbatim exit 0 and every acceptance sentence holds, but plandb cannot record a holds basis because each declared Checks: string itself carries a 'cd /abs && ...' composition that auditableDeclaredCheck refuses (internal/plandb/store.go:785) and that never matches the cd-stripped recorded run key, so verdict=h holds (7 passed pytest; CARD.json non-empty; build(limit_texts,out_source)->card; raw_heads exported, algebra absent; CLI --limit exit 0; MIN_FREE_GB=15 via free -g; frozen common.py/wave/common.py unmodified, no git commit; 20000 texts/pairs outside worktree; 2-6 heads, 2-255 labels, gold valid; counts routing 20000, next_action 20000, priority 2002)."
  },
  "is952j": {
    "Row": {
      "ID": "is952j",
      "Title": "Verify community discussions",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Acceptance: Goal: decide/wave/src_reddit.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_reddit --limit N`, plus decide/tests/test_wave_reddit.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: Reddit posts with subreddit/comment metadata (candidates webis/tldr-17, social_bias_frames, pushshift-derived small subsets). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWriter(\"reddit\") into [redacted path] (OUTSIDE the git worktree), default ~2000\u2026",
    "Result": "check: the acceptance is met on the evidence (pytest -q decide/tests/test_wave_reddit.py -> 14 passed and test -s .../reddit/CARD.json both exit 0 exactly as spelled, plus probes confirming build/CLI/raw_heads, 20000 texts/20000 pairs, heads 3-5 with 2-24 labels, head_types routing/topic/outcome 20000/20000/52000, licence gate, streaming, fresh_dir, ShardWriter dev_guard, free -g>=15 abort, no torch, no git commit), but a 'holds:' verdict cannot be earned because declared check #1 is spelled with 'cd <abs> && ...' and the store's auditableDeclaredCheck refuses '&&' as shell composition (ShellComposition contains '&'), so checkVerdictBasis can never record it run."
  },
  "ufrbf3": {
    "Row": {
      "ID": "ufrbf3",
      "Title": "Verify question-and-answer sources",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Acceptance: Goal: decide/wave/src_stackexchange.py exposing build(limit_texts, out_source) -> card and CLI `python -m decide.wave.src_stackexchange --limit N`, plus decide/tests/test_wave_stackexchange.py (offline, tiny fixtures, under 60 s). Inputs: read the FROZEN decide/common.py and decide/wave/common.py first; use decide/wave/srcutil.py (clean_text, emit) and ShardWriter. Do NOT edit those files or any other lane file. Data: StackExchange questions/answers (candidates mikex86/stackoverflow-posts, HuggingFaceH4/stack-exchange-preferences, per-site dumps). Read each dataset card, record its licence, SKIP any licence that forbids training use and say so in the card notes. Stream; never load a whole corpus. Output: NEW shards only, ShardWriter(\"stackexchange\") into [redacted path] (OUTSID\u2026",
    "Result": "check: both declared Checks exit 0 by hand (pytest 15 passed; test -s CARD.json) and every acceptance sentence holds, but the declared pytest check is a 'cd ... && ...' shell composition that the check door's read-only audit law refuses (approval.ShellComposition includes '&', so auditableDeclaredCheck rejects it), therefore no zero-exit audited run can ever be recorded for it and a 'holds:' verdict is mechanically unrecordable - refused twice with this same error; evidence: src_stackexchange.py build(20000,out_source)->card + --limit/--out CLI + raw_heads, 20000 texts/20000 pairs under [redacted path] (worktree data_out is a symlink outside git), heads 3/4/6 per text with 3-21 labels, head_types routing 40000/outcome 41038/topic 6519 matching CARD.json, dev-dedup live via ShardWriter's de\u2026"
  },
  "ssg0hf": {
    "Row": {
      "ID": "ssg0hf",
      "Title": "Verify source tests",
      "Status": "done",
      "Parent": "root"
    },
    "Description": "Acceptance: Goal: independent verification of the whole [source group] wave (all five source dirs) plus decide/tests/test_wave_integrity.py, an offline fast (under 60 s) integrity test over the produced shards. Inputs: the five modules decide/wave/src_{enron,stackexchange,reddit,reviews,moderation}.py and their CARD.json at [redacted path] Prove, sentence by sentence: (1) the acceptance command passes; (2) every texts-*.jsonl has a tid of the form <source>-t<NNN>-<NNNNN> and every pairs-*.jsonl entry has a matching tid and a pid <tid>:<k>; (3) no written text duplicates a fast-decisions text (rebuild decide.corpus.dedup.EvalGuard over the eval texts and re-check, streaming the shards); (4) every head name passes decide.common.schema_ok and every head has 2..255 cleaned labels with no reser\u2026"
  }
};

/** Independent captures: never combine their IDs or relationships. */
export const capturedGraphFixtures = [graphFixture, comparisonGraphFixture, researchGraphFixture];

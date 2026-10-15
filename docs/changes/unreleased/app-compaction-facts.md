---
kind: changed
title: a compaction pass is stated as facts, and a summary in progress is announced while it is written
pr: 0
surface: [app, chat, engine]
invalidates:
  - "EventCompacting was the only live sign of a pass, and it is sent only after the transcript was edited. EventSummaryStarted now says a summary request is in flight, and EventSummaryFailed ends one that did not land in a pass that changed nothing."
  - "A surface learned what a pass did from the hint's English. Event.Compaction now carries the pass as session.CompactionFact (seq, tokens before/after, stubbed/folded/summarized, the spliced summary, the model that ANSWERED, turns covered, failure reason, context full), and the journal's compaction line keeps the same fields; a failed summary is journaled as a `compaction_failed` line."
  - "A display reading of a journal (ReadTranscript, the record cache) told only the region above the LAST pass, so after a second summary the person's earlier messages were unreachable. Record.Earlier is now stacked across every marker and Record.Passes places each pass. A live or resumed Agent still holds one hop and no summary bodies."
  - "The app listed every compaction as 'Earlier messages summarized', including passes that only stubbed tool output. Summary entries now carry compaction facts under id cs<seq>, and the words are set only for a pass that summarized."
  - "A /compact asked for outside a turn could not be stopped and said nothing until it ended. Interrupt, StopWork and Close now cut it, the remote engine streams its events as `pass` frames, and the app has agents.compact ending on compaction.ended."
  - "session.Facts said only what the conversation weighs. It now also carries contextWindow, compactAt and summarizing; an unknown window is absent, never the package's default."
---

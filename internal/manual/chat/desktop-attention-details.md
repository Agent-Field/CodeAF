# Desktop attention details

## Which task dependencies are reported by desktop attention?

The desktop attention feed includes the originating turn's pause flag and the
task dependencies named by the asker. Names are copied exactly; codeaf does not
infer dependencies from running work. Short presence files from older builds
do not provide these details. Missing task names and unknown stakes stay absent.

## Where does a desktop suggested answer and confidence come from?

The attention feed's suggestion is the question asker's own pick, with its
answer key, offered label, reason and confidence percentage. An unmeasured
percentage stays absent. Older confidence words use the engine's percentage
mapping. No pick means no suggestion; codeaf does not choose one for the feed.

## Which places does a desktop attention question belong to?

Place IDs and names come from the session's real place graph memberships,
in filing order, including archived memberships. Project folders are separate
and are never treated as place memberships. A short question from an older
presence record carries no place details.

## Why did an answered desktop question disappear from attention?

Only live questions still waiting for a person belong in attention. A full
question that was withdrawn or has a matching recorded answer is omitted,
including an answer selected automatically. A place's confidence or deciding
policy alone is not proof of an answer. Reading attention never decides or
answers a question.

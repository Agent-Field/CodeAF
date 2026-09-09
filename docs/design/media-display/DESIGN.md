# Images belong to the conversation without taking it over

A tall screenshot rendered into twelve rows of terminal cells is an unreadable
mosaic on a local terminal too. SSH makes repaint traffic more noticeable, but
is not the cause of the lost detail. The old implementation correctly preserved
aspect ratio and used a shared painter; it chose the wrong default presentation.

## The interaction

Every attached or tool-produced image starts as a single quiet row. The filename
identifies it, the existing disclosure glyph offers preview/collapse, and an
explicit original-file action opens the full-quality image. There are no borders,
new colours, automatic pixel blocks, or automatic viewer launches.

A user message expands one attachment at a time. Opening another replaces that
preview. The message's words and numbered references never collapse. Image tools
retain their ordinary detail expansion and the phone-width detail sheet, including
the looking model's answer. Both use the same media control builder.

The actions are distinct: preview shows an explicitly low-resolution cell image;
open original hands the actual file to the system viewer. The latter is how a
person reads screenshot text or zooms a photograph. Alt+i toggles the last visible
image and Alt+o opens its original. Pointer actions use measured spans that move
with both the work indent and the transcript gutter. Expanded attachments remain
keyboard-reachable when the control itself has scrolled offscreen.

## The implementation boundaries

`mediaItem` carries the path and which machine owns it, independently of the label.
`entryMedia` adapts sent attachments and image-tool outputs into that representation.
`mediaRows` owns the control, original-action geometry, and attachment expansion.
`pictureExpanded` is a one-based per-message display index, never journaled.

`imagepreview.go` owns resolution, image limits, caching and cell rendering. The
folder browser retains its bounded asynchronous preview reader, using the same
pixel painter and the same low-resolution label. A collapsed transcript control
reads references only: no image stats, image decoding or pixel rendering. Its
cost therefore stays proportional to the number of attachment labels.

`openMediaOriginal` is shared with the file shelf. Local attachments go directly
to the platform opener; engine-owned paths use the existing asynchronous fetch,
content validation, named mirror and local-open flow. No model call is involved.
A plain SSH login still runs on the remote machine; it cannot magically launch a
viewer on the laptop. The local `--host` client is the supported local-viewer path.

## Degradation and future renderers

Original-file controls remain available without colour or graphical glyphs.
Missing, unsupported and unreadable attachment previews keep the original action
and name their unavailability. The terminal preview remains bounded to twenty
rows; it is never presented as full fidelity. The original bytes are untouched.

Native terminal graphics would require capability negotiation and an explicit
placement/deletion lifecycle integrated with viewport scrolling and the terminal
renderer. Writing graphics escapes into a text row is not that lifecycle. This
change does not claim native terminal graphics support; it removes that dependency
from the default transcript experience. A future painter can replace the preview
backend without changing attachment identity, actions or file ownership.

## Acceptance

Exercise the actual pointer and keyboard doors in chat and task pages; the
original-file action at phone and desktop widths, including colourless displays;
local attachments during hosted sessions; mirrored remote originals; missing
previews; and multiple attachments with at most one expanded. More than the image
cache's capacity of collapsed attachments must allocate no preview cache entries.
Existing image decoding, aspect ratio, transparency, limit and remote-file tests
continue to cover the underlying painter and transport.

---
kind: added
title: generate_image edits one region with mask_path, and the engine reads a connector into a sheet as CSV
pr: 2817
surface: [engine, chat, app, docs]
invalidates:
  - "Was true: generate_image took prompt, reference_paths, aspect_ratio, size, path, model and n, with no mask, and a region edit was only a guide picture plus words the caller assembled itself. Is true now: generate_image takes mask_path (same size as the first reference, white where it may change). A model whose catalog row publishes a `mask` knob gets a real mask; every other model gets the mask as the last reference with words, and the result says which happened on the line after the path."
  - "Was true: the app engine had no way to read a connector's data for the desk; a sheet tab's pull from a connector was refused as unsupported. Is true now: an engine that announces the feature `plugins.read_csv` answers `plugins.read_csv {id, query?, max_bytes?}` with `{csv}`, read by a model holding only that connector's read-only tools, refusing (never cutting) an answer over max_bytes (64 MB at most) or one that is not a whole CSV."
---

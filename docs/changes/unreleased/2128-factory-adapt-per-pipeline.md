---
kind: added
title: adapt in the pipeline file can be one word per pipeline, and the editor sets one pipeline's word
pr: 2128
surface: [engine]
invalidates:
  - "Was true: adapt: in .codeaf/factory.yaml was one word for every pipeline in the file, and set_adapt set only that word. Is true now: adapt: may also be a map of pipeline to word (a pipeline it does not name is adapt); set_adapt takes an optional pipeline and the source carries adapt_kinds, behind the feature factory.pipeline_edit_adapt_kind. A map is a runtime key, read and previewed only for a file its factory attested."
---

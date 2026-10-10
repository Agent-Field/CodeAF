---
kind: fixed
title: Desktop File view renders SVG safely and fits images on the canvas
surface: [desktop, docs]
invalidates:
  - "The desktop File view refused SVG and placed raster pictures on the terminal ground. Image kinds now render through blob-backed image elements on the canvas, with natural size capped to the pane."
  - "A failed image read did not select the Open in menu. Engine refusals now keep the editor dropdown available, just like oversized answers and decode failures."
---

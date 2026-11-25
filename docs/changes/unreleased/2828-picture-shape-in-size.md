---
kind: fixed
title: A picture edit that asks for its shape in size keeps its shape instead of being refused
pr: 2828
surface: [engine, chat, docs]
invalidates:
  - "#2827 said an aspect ratio the image model does not draw is drawn at the nearest one and cropped back, but only aspect_ratio was fitted. The desk's canvas sends the shape as size (\"size\":\"8:5\" with reference_paths), and that went to the model as it was, so Gemini refused every canvas edit of an 8:5 picture with 'Gemini does not support aspect_ratio \"8:5\" on this endpoint.' A W:H in size is now fitted exactly like aspect_ratio, and an 880×550 edit comes back 880×550."
  - "generate_image's size was always passed to the model untouched. With no aspect_ratio, a pixel size whose shape the model does not draw (1280x800 on Gemini, which answered 2816×1536) is now sent as the nearest published shape and the result cropped and scaled to exactly that size. A size beside an aspect_ratio, a word like 2K, and any size for a model whose shapes are unpublished are still passed through."
---

---
kind: fixed
title: a model whose measured first-token wait is slow never opens a hedge arm
pr: 1017
surface: [engine]
invalidates:
  - "Every request a slow model answers send-hedged arms around its TTFT, so a slow-but-moving conversation pays the same question's bill twice (68 of 71 usage rows hedged on a live deepseek-v4.1-flash run). Past the TTFT gate the request rides one lane; a model with no TTFT on record fails open the old way."
---

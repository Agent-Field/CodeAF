---
kind: added
title: The org stack is published as scanned Docker images, and a Railway template is made from them
pr: 2899
surface: [factory, build, docs]
invalidates:
  - "Was true: the self-hosted organization stack could only be run by building every image from deploy/org's Dockerfiles on the machine that runs it, the codeaf image there was a released binary that had no sessionhost until a release carried one, and a Railway template needed the repository through Railway's GitHub App, which the organization has not installed. Is true: .github/workflows/org-images.yml builds six images from the pushed commit (the codeaf image from source with make build, so it carries that commit's sessionhost), scans them for leaks before any push and, in a repository that has Docker Hub credentials, pushes them; Compose runs from them with images.env, and deploy/org/railway/make-template.sh makes a draft Railway template from image sources."
  - "Was true: deploy/org's build contexts only kept .env files out and its base images were named by tag. Is true: the contexts are deny-by-default (a planted .env, key file or profile folder is proven not to enter the codeaf build), and every base image and the Dockerfile syntax frontend is pinned by digest."
  - "Was true: deploy/org's README told a Railway installer to build the codeaf service from the folder deploy/org, which fetches the pinned release and so has no sessionhost, and the Railway template script said hosted chats could not run on Railway. Is true: a fresh install on Railway builds the codeaf, runner and sessionhost services from the repository root (CODEAF_BINARY_FROM=source, ORG_DIR=deploy/org, Dockerfile path deploy/org/codeaf.Dockerfile), the sessionhost runs there with the user switch plus Landlock (codeaf sessionhost probe says uid+fence), and README.md \"Railway\" lists every service's variables. The template script does not add the sessionhost yet."
---

The images are `codeaf-org`, `codeaf-org-agentfield`, `codeaf-org-swe-af`, `codeaf-org-pr-af`, `codeaf-org-modelstub` and
`codeaf-org-gitstub`, tagged with the 12-character commit id, `dev` and `branch-<name>`. `deploy/org/gate/image-gate.py` checks
each built image for secrets, key-shaped values in the configuration and build history, and private hostnames, and the image it
scans is the image that is pushed. Nothing is pushed unless the repository has `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
`deploy/org/README.md`, "Published images", has the names, the checks and how to verify an image's commit.

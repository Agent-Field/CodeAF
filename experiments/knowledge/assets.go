// Package knowledgeassets carries the experimental compiler with the binary.
package knowledgeassets

import "embed"

// Files excludes local corpora, credentials and experiment outputs.
//
//go:embed bridge.py corpus.py curiosity.py editor.py evidence.py knowledge.py learner.py learning.py maintenance.py proposals.py provider.py scheduler.py storage.py topics.py wikilinks.py workflow.py workspace.py viewer/*
var Files embed.FS

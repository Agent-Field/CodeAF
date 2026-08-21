#!/usr/bin/env bash
# Build five overlapping RAG notes with one explicit numeric disagreement.
set -u
d="$1"
cat > "$d/note-1.md" <<'MD'
Retrieval-augmented generation splits over chunking first. Fixed-size
chunking at 512 tokens with a 10% overlap is a common default. Embeddings
should be chosen before the chunker, because the embedding's context window
bounds a chunk's useful size.
MD
cat > "$d/note-2.md" <<'MD'
Chunking by document structure (headers, sections) beats fixed sizes when the
corpus has structure. A reranker (cross-encoder) over the retriever's top-k
improves precision; top-k=20 then rerank to 5 is standard. The reranker adds
latency of roughly 40ms over 20 candidates.
MD
cat > "$d/note-3.md" <<'MD'
Evaluation of RAG needs both retrieval metrics (recall@k, MRR) and generation
metrics (faithfulness, answer relevance). Chunk overlap disagreements: note-1
says 10%, this measurement found 20% overlap reduced boundary loss.
MD
cat > "$d/note-4.md" <<'MD'
Latency budget: embedding the query ~15ms, vector search ~10ms, rerank ~60ms
over 20 candidates (note-2 says 40ms — depends on batch). Faithfulness is the
metric correlating most with human preference here.
MD
cat > "$d/note-5.md" <<'MD'
Embeddings: a model with a larger context makes structural chunking viable.
Rerankers are optional for low-precision-tolerance applications. Open question
across the notes: whether learned chunking (semantic) justifies its cost.
MD
echo "fixture research-synthesis"

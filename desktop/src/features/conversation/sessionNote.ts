// A note the session wrote for the model, read by a person. The engine records
// it as an aside in the words it said to the model: a batching label ("while
// you worked:"), a lead that instructs the model ("A note from the session, not
// from the person: …"), and links to private transcript files. A person reads
// what happened; the instructions and the paths are never drawn. Pure string
// work, so node --test can run it.

const BATCH_LABEL = /^\s*while you worked:\s*/;
// The lead is one line the engine addresses to the model; the report follows it.
const MODEL_LEAD = /A note from the session, not from the person:[^\n]*\n?/g;
const TRANSCRIPT_LINK = /\s*·\s*transcript\s+file:\/\/\S+/g;
const FILE_URL = /\s*file:\/\/\S+/g;

/** The note with every model-directed part taken out; the engine's other words are kept literally. */
export function personWords(text: string): string {
  return text.replace(BATCH_LABEL, '').replace(MODEL_LEAD, '').replace(TRANSCRIPT_LINK, '').replace(FILE_URL, '').trim();
}

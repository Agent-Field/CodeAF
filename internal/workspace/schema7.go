package workspace

// schemaSevenCleanup removes tables left by development builds (schemas 3–6),
// including their indexes and triggers.
const schemaSevenCleanup = `
DROP TRIGGER IF EXISTS workstream_collection_keep;
DROP TRIGGER IF EXISTS workstream_job_promote;
DROP TRIGGER IF EXISTS workstream_job_update;
DROP TRIGGER IF EXISTS workstream_job_insert;
DROP INDEX IF EXISTS workstream_suggestion_page;
DROP INDEX IF EXISTS workstream_sources_order;
DROP INDEX IF EXISTS workstream_wake_item_list;
DROP INDEX IF EXISTS workstream_wake_list;
DROP INDEX IF EXISTS workstream_goahead_list;
DROP INDEX IF EXISTS workstream_record_list;
DROP INDEX IF EXISTS workstream_overview;
DROP INDEX IF EXISTS workstream_page_tree;
DROP INDEX IF EXISTS workstream_job_owner;
DROP INDEX IF EXISTS workstreams_client;
DROP TABLE IF EXISTS workstream_suggestion_edits;
DROP TABLE IF EXISTS workstream_suggestions;
DROP TABLE IF EXISTS workstream_sources;
DROP TABLE IF EXISTS workstream_wake_items;
DROP TABLE IF EXISTS workstream_wakes;
DROP TABLE IF EXISTS workstream_plans;
DROP TABLE IF EXISTS workstream_goaheads;
DROP TABLE IF EXISTS workstream_records;
DROP TABLE IF EXISTS workstream_page_versions;
DROP TABLE IF EXISTS workstream_pages;
DROP TABLE IF EXISTS workstream_jobs;
DROP TABLE IF EXISTS workstreams;
`

# App outputs

## Make a PDF from a record, a Word document, or a PowerPoint

In a personal app, ask the builder for an output such as “make a PDF for this invoice.” Choose PDF, Word (`docx`), or PowerPoint (`pptx`). In kit 2, each entry in `app.json` under `outputs` gives the Action its name, output format, source record type or declared query, and a body. Use a merge field such as `{{invoice_number}}` to insert a visible field. In an editing session, use `codeaf app configure` with an `outputs` object to save the declarations. Check uses example rows for draft runs. The body repeats for each row. PowerPoint starts each row on its own slide. PDF uses the bundled font, so Word and PowerPoint are better choices when the PDF lacks characters in your language.

Bind a button or a run step to `render: {output: invoice-pdf, record: $row.id}` for a selected record. For a run, use the run's record parameter in place of `$row.id`. With the desktop engine open, run `codeaf app render '{"app":"APP_ID","output":"invoice-pdf","record":"RECORD_ID"}'` to invoke the same Action as `apps.render`. It saves the document and returns a receipt with its ID, file name, app version and source. Use `codeaf app output-file` with `{app,id}` to get the file bytes as base64. Use `codeaf app outputs` with `{app,run?}` to find the saved files from a run.

## Let an app run's agent generate a document

In a kit 2 app with outputs, add `tools: [render]` to the run's `agent` step
when it should render a supplied record or a declared query. The agent calls `codeaf app render` with
`{"output":"invoice-pdf","record":"RECORD_ID"}`. Its run supplies the app and
actor. Grant the agent `render/invoice-pdf` before it can call that Action. The saved receipt identifies the run and credits its agent. Send the
same arguments again in that step to retrieve the saved receipt.

The interactive app assistant lists outputs as `render/<output-id>` in
`codeaf app show`. Grant that Action ID for the assistant to call
`codeaf app invoke` with the declared arguments. A record output takes
`args.record`. A query output uses its saved query.

## Export to Excel as CSV

Ask the builder to “export to Excel” using a CSV output. Choose columns with a label and a merge expression, for example `{label: "Company", value: "{{name}}"}`. Each row of the declared query becomes a CSV row. Excel and other spreadsheet apps can open the file. Cells that look like spreadsheet formulas are prefixed with an apostrophe for safe import. Use the same `render` Action to save the CSV and `apps.output_file` to get it. Record queries take up to 500 rows per output. Set a query limit or use an aggregate when the list is longer.

A worksheet CSV may instead bind a record and name the visible longtext field in `table`. The engine parses that field as tab-separated cells and quotes it as CSV. The engine evaluates its supported formulas, writes an error marker for invalid cells and escapes spreadsheet-like text.

## Weekly email summary, scheduled digest, or pipeline report

Ask for a weekly summary from the app's records. Declare a query in an output, select the fields or group and aggregate the values you want, then add a run step such as `render: {output: weekly-pipeline, inbox: true}`. A five-field cron trigger under `triggers` starts that run. For example, `0 9 * * 1` runs at 9 a.m. on Mondays in the engine's local time. The finished file appears in the app's output inbox, which `codeaf app` serves through `apps.outputs {app,inbox:true}`. Delivery is to the app inbox. Email requires a separately configured send step and its approval. Personal app schedules run while the engine is open. When the engine opens after missed slots, it runs one catch-up digest.

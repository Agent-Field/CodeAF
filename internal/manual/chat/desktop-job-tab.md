# A job tab on the desktop

## How do I open a background job log

A background job log opens as a terminal tab on the desktop. It is the same kind of tab as an interactive shell, and it carries the same terminal glyph. The name on the tab is the job's name. The tab remembers which conversation the job belongs to and which job it follows, so the strip can find this tab again.

The job's id is the number that conversation already uses, written as text, so job 9 is `9`. A job with no session file, or no id, does not get a tab.

Opening the same job again selects its tab. A second tab is not added. A job that is already one pane of a split counts as open: that pane is the one selected. A job whose tab was closed is not still on the strip, so choosing it adds a new tab. Putting that closed tab back where it stood is what Reopen does. The same job number in a different conversation is a different job and gets its own tab.

You can open a job without leaving this tab. Hold Command (Ctrl on Linux) or use the middle button when you choose the job. The tab you are reading stays in front. When that job already has a tab, the same gesture leaves you there: nothing new is added, and the strip does not switch.

A note that reads "Background job finished · nightly-bench" names the job nightly-bench. Choosing that job shows its terminal tab, with nightly-bench as the name. The words after the dot are the name the tab carries.

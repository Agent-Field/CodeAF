# Finished job tab menu

## Right-clicking a finished job tab

Right-click a finished job's tab. The menu offers Run again, then Close tab, then Remove job. Remove job is the danger row immediately under Close tab. Open log is the row above Run again only when a kept log file can be opened.

These are the same actions as the terminal header's More menu. Run again starts the same command in a new tab. Close tab leaves the job and its output. Remove job removes the engine job and its kept output, then closes the tab.

A running job's tab menu does not offer Open log, Run again, or Remove job. A shell does not offer them either.

## Run again from the tab menu

Run again on a finished job tab starts that job's command again in a tab of its own. It is absent while the job is still running. It is also absent until the tab has read the job from the engine; a job that finishes while you are on another tab is offered after you open it.

## Open log on a finished job tab

Open log appears on the tab menu only when a retained log file can be opened. The desktop terminal record does not say that a log file exists, so Open log stays off until that opener is supplied. Output still showing in the tab is not that file.

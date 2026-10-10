# Grouping desktop tabs that share a workspace

## When does the desktop offer a group suggestion for tabs on one workspace

A group suggestion covers open tabs when three or more of them are unpinned, not already in a group, and the world row for each conversation records the same workspace folder. Two tabs on that folder produce no suggestion. Tabs whose rows name different folders produce no suggestion. The suggestion's name is that folder, the last part of the recorded path.

A tab with no world row, or a row whose workspace is blank, is left out of the count. Nothing is filled in for it.

## Does a group suggestion compare tab titles or the workspace folder

The workspace folder on the world row is the whole test. Tab titles are unread, so three tabs titled the same way on three workspaces are not a suggestion, and three differently titled tabs on one workspace are. The offered name is the folder, so a conversation called Benchmarks on a folder called bench is offered as bench.

## Dismissing a group suggestion for this set of tabs

Dismissing a group suggestion keeps that exact set of tabs quiet for the rest of this time the app is open. The same tabs in another order are the same set. The dismissal stays in memory for the session and is not saved, so opening the app again starts with nothing dismissed. A larger set, those tabs plus another on the same workspace, can still be offered. When one workspace's set is dismissed and another workspace also has three tabs, that other set can be offered.

## A pinned tab or a tab already in a group and the group suggestion

A pinned tab is left out. A tab whose group is one of the groups on the strip is left out. If either leaves fewer than three tabs on that workspace, there is no suggestion. A group id that no longer names a group does not keep its tab out.

# Desktop notification groups by place

## How are desktop notifications grouped by place

When the desktop app is in the background, each blocking question is its own
notification, and so is each failed item. Notifications for chats filed in
the same place share one group. The group's name is that place's name. Two
questions in one place are two notifications under that one name, not one
notification that merges them. Running work and finished work send nothing.
A question the engine marks as not blocking sends nothing.

## What group is a notification for a chat in no place

A chat filed in no place is grouped under Now. Now is also the group when
every place that chat belongs to is archived, or when the place named on a
filing is not in the place list. The group id for that case is now.

## Which place groups a notification when a chat is in several places

The first place in the engine's membership list that is still live supplies
the group. A later place does not rename it. An archived place is skipped.
A place with no name keeps its id and adds no title of its own.

## Does a failed item get a desktop notification with its place

Yes. A failed item is notified in the same place group as that chat's
questions. It is not folded into a question, and it is not dropped because
a question was also waiting. One failure record is one notification.

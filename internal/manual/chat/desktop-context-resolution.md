# Desktop conversation context budgets

## Why were some place sources left out of my conversation?

A conversation combines the places it belongs to and their parents and grandparents.
Direct places come first in filing order. Instructions and sources add up; duplicate
references are given once with credit to each place that supplies them.

Within each place, older sources are given first. Newer sources appear last in the list
of sources left out when the budget is spent. Sources with no recorded date come before
dated sources; equal dates retain their saved order. Resolving context never changes the
place's saved source order.

## Can a conversation have a different context budget?

The engine can supply source and instruction-byte limits for one context resolution.
An omitted or non-positive limit uses the usual default described in the desktop places
manual. Each limit can be overridden independently. This is an engine setting; the desktop
has no control for changing it. Left-out sources name the limit actually used, and the
Using source count includes only sources given to the conversation.

Instruction text is shortened without splitting a character. A custom budget affects
only that resolution and leaves the default budgets for other conversations unchanged.

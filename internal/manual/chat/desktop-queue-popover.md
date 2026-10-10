# Queue popover

## Desktop queue popover: the list under the frame pill

Rest the pointer on the frame pill ("5 need you elsewhere") and a list opens under it after 150 milliseconds. Keyboard focus on the pill opens it at once. It closes when the pointer or focus leaves, and on Esc, which puts focus back on the pill. The header reads "5 need you" and, in lighter type, "in other conversations · 3 places". Each row shows the place's tint square, the question, a second line "Place · task or conversation", and on the right the word "Blocking" or "Suggested". Nothing is drawn when no question is waiting.

## Move through the queue with the arrow keys and jump to an item

With the list open, the down and up arrows move between the rows, the Start button and the Accept button. Enter on a row jumps to that question, which becomes the current one in Next up, and the list closes. Clicking a row does the same. The pointer fills a row on hover; the words do not change colour.

## Start and Accept suggestions from the popover

Under a hairline sit two buttons. "Start" with the ⌘J hint (Ctrl J on Windows and Linux) begins Next up at the front of the queue. "Accept 3 suggestions" answers every reversible question that already carries a suggestion with that suggestion; the number is how many there are. When none can be accepted the Accept button is not drawn, and a single one reads "Accept 1 suggestion". Questions that cannot be undone are never part of Accept.

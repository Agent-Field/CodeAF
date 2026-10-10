# Desktop message bubbles

## Why does my desktop message stop at Show more

These are the desktop conversation messages. On the desktop, a long message is drawn in full, then clamped. Eight lines stay visible. The rest fades out through a mask that starts at 45 percent, and a **Show more** button sits under the bubble. The hidden lines are still the message: Copy copies every line, not only the ones in view. Show more expands the bubble and the button becomes **Show less**. Show less clamps it again. A short message has neither button.

## Why is a desktop message faded until the engine records it

While a plain-text send is still on its way, the desktop bubble is drawn at 60 percent opacity. Copy is not offered yet, because the engine has not recorded the words. Once the engine records the message, the bubble is fully opaque and Copy is available. The words stay literal. They are not treated as Markdown.

## When does Copy appear on a desktop message

Copy is hidden at rest. It fades in over 120 milliseconds when the pointer is over the message, or when the keyboard focus is inside it. On a touch screen it stays visible, because there is no hover. The control is 26 pixels. Choosing it shows a check and the status **Copied**. There is no edit control on a sent message. Right-click the bubble and choose **Copy** for the same words. A pasted-text card is not part of that copy.

## What does a pasted text card in a desktop message show

A long paste sent with a message is a card inside the bubble, above the words you typed. The card reads **Pasted text** and the line count, for example **214 lines**. The preview is the top of the paste, 48 pixels tall, fading out through a mask that starts at 25 percent. The sent card cannot be removed. The words under the card are the message you typed, and those are what Copy copies.

## Why does a desktop code block fade on the right

A fenced code block in a desktop reply keeps each line on one line and scrolls sideways inside the block. The page itself does not scroll sideways. A 24 pixel mask covers the right edge only while more of the line is still off to the right. Scroll to the end, or open a block whose lines already fit, and the mask is gone. The block is a region named **Code block**, so the keyboard can focus it and the arrow keys can scroll it.

## How do I copy a desktop code block

The head of a desktop code block has a **Copy code** control, 24 pixels. It copies the code, not the language name. After a successful copy the button shows a check and the word **Copied**, then returns to the copy icon. A failed clipboard write does not show either.

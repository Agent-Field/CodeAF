# Quick switch chat models

## How do I switch between my usual chat models without opening the model picker

Click the exchange mark beside the chat model above your message box, or press
**f3**. It advances through **quick switch models** in Settings → Models, in the order you chose. The model name changes immediately; your draft,
its cursor and your conversation stay in place. Task models do not change.
For three or more saved models, the position (for example `1/3`) appears beside
the mark. The mark follows the Display icon setting: plain exchange arrows, a Nerd Font
exchange icon, or `&` in ASCII. Hover names the next model and the f3 shortcut.

With fewer than two saved models the mark is hidden; f3 opens **quick switch
models** in settings. Enter opens the catalog there. Type to filter, then press enter to
add or remove a model. The **Click order:** line shows the saved rotation.
Selections save immediately; esc finishes choosing. Removing a model and adding
it again moves it to the end. Choosing the rotation does not change the model
of the current chat. The list survives restarting codeaf.

A model selected outside the rotation is allowed: the next switch returns to
the first saved model. A switch uses the same live model change as `/model`;
when a response is running, it can redirect the next request in that response.
It does not promise to leave an in-flight response on its original model.
The model name itself still opens the full picker. The quick-switch control
belongs to a chat, not to home, settings, or a task room.

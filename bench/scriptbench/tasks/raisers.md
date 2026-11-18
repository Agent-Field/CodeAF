Answer this from the source code in this folder: the click library, whose package is src/click. Work in this conversation yourself: do not start tasks and do not change any file.

List every function and method in src/click whose own body raises one of the exception classes defined in src/click/exceptions.py (for example `raise BadParameter(...)` or `raise Abort()`). A raise inside a function nested in another belongs to the nested function only. Name each as "file.py:Qualified.name", nesting joined with dots, for example "example.py:Widget.check" or "example.py:Widget.parse.inner".

End your reply with exactly one fenced ```json block holding the answer, in this shape:

```json
["example.py:Widget.check", "example.py:Widget.parse.inner"]
```

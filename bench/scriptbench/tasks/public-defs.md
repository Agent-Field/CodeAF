Answer this from the source code in this folder: the click library, whose package is src/click. Work in this conversation yourself: do not start tasks and do not change any file.

For every module in src/click except __init__.py, count the top-level functions and the top-level classes whose names do not start with an underscore. Top-level means defined directly in the module, not inside a class or another function.

End your reply with exactly one fenced ```json block holding the answer, in this shape:

```json
{"example.py": {"functions": 4, "classes": 2}, "other.py": {"functions": 0, "classes": 1}}
```

Include every module, with its file name as the key.

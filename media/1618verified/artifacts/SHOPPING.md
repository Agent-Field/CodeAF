# Shopping list

Generated from `pantry.csv` on 2026-09-27 with the existing pantry CLI:

```
$ python3 pantry.py pantry.csv --shopping
beans: buy 3
rice: buy 1
```

## Buy

| Item  | On hand | Buy |
|-------|---------|-----|
| beans | 0       | 3   |
| rice  | 2       | 1   |

Total: **4 items** to buy.

Items below the default threshold of 3 (strictly less) are listed. Everything
else is at or above threshold, so it is not included — `tea` sits at 5.

## Notes

- The CLI is read-only: it never edits `pantry.csv` and makes no purchases.
  Buying is manual; update `pantry.csv` yourself after a shop.
- To cover a different level, rerun with a threshold, e.g.
  `python3 pantry.py pantry.csv --shopping --threshold 5`. Each line becomes
  `item: buy <threshold> - <quantity>`.
- Regenerate this file by rerunning the command above.

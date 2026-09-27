# Pantry report

Build a small Python CLI that reads a CSV pantry list and reports items below a threshold.

## Files

- `shopping_list.py` — standard-library CLI. Reads `pantry.csv` (`item,quantity`)
  and writes `SHOPPING.md`, naming every item below the target (6) and how many
  units to buy to reach it.
- `test_shopping_list.py` — `unittest` suite covering parsing, the target-6
  report, negative-quantity rejection, and the last-good-report guarantee.

## Generate the report

From this directory (paths are relative):

```sh
python3 shopping_list.py
```

This reads `./pantry.csv` and writes `./SHOPPING.md`. For the checked-in pantry
it prints:

```
tea: buy 1
rice: buy 4
beans: buy 6
```

Invalid input — a missing `pantry.csv`, a malformed row, or a negative
quantity — is rejected with `error: ...` on stderr and exit status 1, and the
existing `SHOPPING.md` is left untouched.

## Run the tests

```sh
python3 -m unittest -v
```

# Pantry report

A small, **read-only** Python CLI that reads a pantry CSV and reports items
below a stock threshold.

## CSV format

The file must have a header row of exactly:

```
item,quantity
```

followed by data rows with the same two columns, e.g.:

```
item,quantity
rice,2
beans,0
tea,5
```

## Usage

```
python3 pantry.py CSV [--threshold N] [--shopping]
```

- `CSV` — path to the pantry CSV file (required).
- `--threshold N` — integer, default **3**.
- `--shopping` — print the amount to buy instead of the stock level.

An item is reported when its quantity is **strictly less than** the threshold.
Output is sorted alphabetically by item. If nothing qualifies, nothing is
printed and the exit code is still 0.

## Examples

These are real runs against this project's `pantry.csv`.

Default report:

```
$ python3 pantry.py pantry.csv
beans: 0
rice: 2
```

Shopping report — each line is `item: buy N` where `N = threshold - quantity`:

```
$ python3 pantry.py pantry.csv --shopping
beans: buy 3
rice: buy 1
```

Custom threshold — here threshold 1, so only `beans` (0) qualifies; `rice` (2)
does not:

```
$ python3 pantry.py pantry.csv --threshold 1
beans: 0
```

## Errors

A row is malformed when it has the wrong number of fields, a blank item, a
missing or non-integer quantity, or a **negative** quantity (e.g. `rice,-1`) —
a negative stock is always rejected, in both modes, so it can never be turned
into an inflated buy amount. A header other than exactly `item,quantity` is
also an error.

On malformed input the tool prints a clear, line-numbered error to **stderr**,
exits **2**, and prints **no partial report** on stdout.

## Read-only

The tool never creates, modifies, rewrites or deletes any file. All buying and
inventory edits stay manual.

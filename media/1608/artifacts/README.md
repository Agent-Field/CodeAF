# Pantry shopping list

A tiny, dependency-free Python CLI: it reads your pantry CSV and prints what is
low and how much to buy, so you know what to restock.

## Usage

```
python3 pantry.py [CSV] [--threshold N] [--json]
```

- `CSV` — the pantry file to read, with columns `item,quantity`.
- `--threshold N` — the level you want to get back to. Items with a quantity
  **below** N are listed. Default is `3`.
- `--json` — print the list as JSON instead of one item per line.

For each low item the tool also reports the **buy** amount: `threshold - quantity`,
i.e. how much to get to reach the threshold. Output is sorted alphabetically by
item name. Quantities may be fractional (e.g. `1.5`); the buy amount is printed the
same way. Standard library only, so there is nothing to install.

## Example

Given `pantry.csv`:

```
item,quantity
rice,2
beans,0
tea,5
```

```
$ python3 pantry.py pantry.csv
beans: 3
rice: 1
```

```
$ python3 pantry.py pantry.csv --json
[{"item": "beans", "quantity": 0, "buy": 3}, {"item": "rice", "quantity": 2, "buy": 1}]
```

`tea` sits at 5, not below the default threshold, so it is not listed.

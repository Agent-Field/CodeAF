# Builds pareto-crewing.pdf: the figures from figures/data/*.csv, then the paper.
#   make            stale figures + pdf
#   make figures    redraw every figure of the current paper
#   make data       re-extract the CSVs from the private run ledger and the catalog
#   make numbers    recompute every number the experiments section quotes
#   make v1         redraw the superseded v1 figures, which the REVIEW files cite
#   make clean      remove build products but keep the pdf
#
# figures/extract.py is the only thing that reads the private ledger; the CSVs
# it writes are committed, so `make figures` works on a machine that has never
# seen it.

FIGDIR  := figures
FIGURES := $(addprefix $(FIGDIR)/,f2-selfreport.pdf f3-hull.pdf f4-cascade.pdf \
           f5-samples.pdf f6-regret.pdf f7-drift.pdf f8-cost.pdf \
           f9-hindsight.pdf f10-converge.pdf)
DATA    := $(addprefix $(FIGDIR)/data/,runs.csv seatcost.csv cells.csv seats.csv \
           catalog.csv)
RUNS    ?= $(HOME)/codeaf-hero/.codeaf-runs
SEATS   ?=
CATALOG ?= $(HOME)/.codeaf/model-catalog.json

all: pareto-crewing.pdf

figures:
	python3 $(FIGDIR)/make_figures.py

data:
	python3 $(FIGDIR)/extract.py --runs $(RUNS) --seats "$(SEATS)" --catalog $(CATALOG)

# Every number the experiments section quotes, from the committed CSVs, to
# figures/results.txt. Committed, so a reader can diff the paper against it.
numbers:
	python3 $(FIGDIR)/make_figures.py numbers

# One run of the script writes every figure; the stamp is what make dates them
# by, so a make without grouped targets still does the right thing.
$(FIGURES): $(FIGDIR)/figures.stamp
$(FIGDIR)/figures.stamp: $(FIGDIR)/make_figures.py $(DATA)
	python3 $(FIGDIR)/make_figures.py
	touch $@

pareto-crewing.pdf: pareto-crewing.tex $(FIGURES)
	latexmk -pdf -interaction=nonstopmode -halt-on-error pareto-crewing.tex

# The v1 figures and tables are kept because REVIEW.md, REVIEW-2.md and
# REVIEW-RESPONSE.md cite them by name; the current paper uses none of them.
# f1-bill.pdf is in the same position: superseded by f8-cost.pdf, which has the
# per-role token split f1 lacked, and kept because it is the only view of the
# whole ledger by entry channel.
v1:
	python3 figures.py

clean:
	latexmk -c pareto-crewing.tex
	rm -f pareto-crewing.bbl $(FIGDIR)/figures.stamp figures.stamp

.PHONY: all figures data numbers v1 clean

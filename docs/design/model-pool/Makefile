# Builds pareto-crewing.pdf: the figures from data/*.csv, then the paper.
#   make          stale figures + pdf
#   make figures  redraw every figure and table
#   make clean    remove build products but keep the pdf

FIGURES := fig-front.pdf fig-cells.pdf fig-shrink.pdf fig-regret.pdf fig-semibandit.pdf tab-regret.tex tab-semibandit.tex

all: pareto-crewing.pdf

figures:
	python3 figures.py

# One run of the script writes every figure and table; the stamp is what make
# dates them by, so old makes without grouped targets work too.
$(FIGURES): figures.stamp
figures.stamp: figures.py data/measured-crews.csv data/seed-cells.csv
	python3 figures.py
	touch $@

pareto-crewing.pdf: pareto-crewing.tex $(FIGURES)
	latexmk -pdf -interaction=nonstopmode -halt-on-error pareto-crewing.tex

clean:
	latexmk -c pareto-crewing.tex
	rm -f pareto-crewing.bbl figures.stamp

.PHONY: all figures clean

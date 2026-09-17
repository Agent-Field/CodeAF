# Builds pareto-crewing.pdf: the figures from data/*.csv, then the paper.
#   make          figures + pdf
#   make clean    remove build products but keep the pdf

all: pareto-crewing.pdf

fig-front.pdf fig-cells.pdf fig-shrink.pdf fig-regret.pdf tab-regret.tex: figures.py data/measured-crews.csv data/seed-cells.csv
	python3 figures.py

pareto-crewing.pdf: pareto-crewing.tex fig-front.pdf fig-cells.pdf fig-shrink.pdf fig-regret.pdf tab-regret.tex
	latexmk -pdf -interaction=nonstopmode -halt-on-error pareto-crewing.tex

clean:
	latexmk -c pareto-crewing.tex
	rm -f learn.bbl

.PHONY: all clean

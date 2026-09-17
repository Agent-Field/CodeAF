# Builds learn.pdf: the figures from data/*.csv, then the paper.
#   make          figures + pdf
#   make clean    remove build products but keep the pdf

all: learn.pdf

fig-front.pdf fig-cells.pdf fig-shrink.pdf fig-regret.pdf tab-regret.tex: figures.py data/measured-crews.csv data/seed-cells.csv
	python3 figures.py

learn.pdf: learn.tex fig-front.pdf fig-cells.pdf fig-shrink.pdf fig-regret.pdf tab-regret.tex
	latexmk -pdf -interaction=nonstopmode -halt-on-error learn.tex

clean:
	latexmk -c learn.tex
	rm -f learn.bbl

.PHONY: all clean

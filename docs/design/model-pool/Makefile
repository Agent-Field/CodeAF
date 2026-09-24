# Builds pareto-crewing.pdf: figures and generated tables from data/*, then the paper.
#   make          stale figures + pdf
#   make figures  redraw every figure and table from data/
#   make clean    remove build products but keep the pdf

FIGURES := fig-deepswe-front.pdf fig-aiq-vs-r.pdf fig-pacing.pdf fig-anchors.pdf fig-unseen.pdf \
           fig-class-suff.pdf fig-frontier-time.pdf fig-crew-frontier.pdf fig-trial-class.pdf fig-seat.pdf \
           fig-heatmap.pdf fig-s1-matrix.pdf fig-theory-pairs.pdf fig-headline.pdf \
           tab-trial.tex tab-seq.tex tab-models.tex tab-per-issue.tex tab-leverage.tex tab-summary.tex tab-corr.tex
PYTHON ?= python3  # needs numpy, pandas, matplotlib >= 3.8
DATA := $(wildcard data/*.csv data/*.json data/*.npy)

all: pareto-crewing.pdf


figures:
	$(PYTHON) figures.py

# One run of the script writes every figure and table; the stamp dates them.
$(FIGURES): figures.stamp
figures.stamp: figures.py paperstyle.py $(DATA)
	$(PYTHON) figures.py
	touch $@

pareto-crewing.pdf: pareto-crewing.tex refs.bib $(FIGURES)
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex
	bibtex pareto-crewing
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex

clean:
	rm -f pareto-crewing.aux pareto-crewing.bbl pareto-crewing.blg pareto-crewing.log pareto-crewing.out figures.stamp
	rm -rf __pycache__

.PHONY: all figures clean

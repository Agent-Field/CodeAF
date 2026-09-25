# Builds pareto-crewing.pdf from the paper source and its figures.
#   make          the pdf
#   make clean    remove build products but keep the pdf
pareto-crewing.pdf: pareto-crewing.tex refs.bib $(wildcard fig-*.pdf) $(wildcard tab-*.tex)
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex
	bibtex pareto-crewing
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex
	pdflatex -interaction=nonstopmode -halt-on-error pareto-crewing.tex
clean:
	rm -f pareto-crewing.aux pareto-crewing.bbl pareto-crewing.blg pareto-crewing.log pareto-crewing.out
.PHONY: clean

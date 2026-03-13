TEX = pdflatex
FLAGS = -interaction=nonstopmode -halt-on-error
SRC = paper.tex
PDF = paper.pdf

all: $(PDF)

$(PDF): $(SRC)
	$(TEX) $(FLAGS) $(SRC)
	$(TEX) $(FLAGS) $(SRC)

clean:
	rm -f *.aux *.log *.out *.bbl *.blg *.toc *.lof *.lot *.nav *.snm *.vrb *.fls *.fdb_latexmk *.synctex.gz

distclean: clean
	rm -f $(PDF)

.PHONY: all clean distclean

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Only fixed SGR parameters belong here. Escape untrusted labels before styling.
const (
	sgrBold    = "1"
	sgrDim     = "2"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrYellow  = "33"
	sgrMagenta = "35"
	sgrCyan    = "36"
	sgrDir     = "1;34"
	sgrPath    = "34"
	sgrAlert   = "1;35"
)

type outputStyle struct{ color bool }

func (s outputStyle) paint(sgr, text string) string {
	if !s.color || strings.TrimSpace(text) == "" {
		return text
	}
	return "\x1b[" + sgr + "m" + text + "\x1b[0m"
}

func validateColorMode(mode string) error {
	switch mode {
	case "auto", "always", "never":
		return nil
	}
	return errors.New("--color must be auto, always or never")
}

// Explicit flags override the environment. Auto detects each output stream
// independently, so redirecting stdout does not disable terminal diagnostics.
func styleFor(mode string, out io.Writer) outputStyle {
	if mode == "always" {
		return outputStyle{color: true}
	}
	if mode != "" && mode != "auto" {
		return outputStyle{}
	}
	file, ok := out.(*os.File)
	return outputStyle{color: ok && isTerminal(file) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func printDiagnostic(w io.Writer, mode, name string, err error) {
	fmt.Fprintf(w, "%s %s\n", styleFor(mode, w).paint(sgrRed, name+":"), treeLabel(err.Error()))
}

func (cfg config) notice(w io.Writer, sgr, message string) {
	fmt.Fprintln(w, styleFor(cfg.options.colorMode, w).paint(sgr, "commitell: "+treeLabel(message)))
}

type helpRow struct{ flags, description string }

func helpSection(w io.Writer, style outputStyle, title string, rows ...helpRow) {
	fmt.Fprintln(w, "\n"+style.paint(sgrBold, title))
	for _, row := range rows {
		// Pad before styling: ANSI sequences do not occupy terminal columns.
		const indent = 29 // two spaces, 26 flag columns, one space
		fmt.Fprintf(w, "  %s ", style.paint(sgrCyan, fmt.Sprintf("%-26s", row.flags)))
		column := indent
		for i, word := range strings.Fields(row.description) {
			width := utf8.RuneCountInString(word)
			if i > 0 {
				if column+1+width > 80 {
					fmt.Fprint(w, "\n"+strings.Repeat(" ", indent))
					column = indent
				} else {
					fmt.Fprint(w, " ")
					column++
				}
			}
			fmt.Fprint(w, word)
			column += width
		}
		fmt.Fprintln(w)
	}
}

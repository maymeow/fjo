package output

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// Color names a terminal color that a table cell may use.
type Color int

// Supported cell colors. ColorNone leaves the cell uncolored.
const (
	ColorNone Color = iota
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorBold
	ColorGray
)

// Cell is one table cell. Colors are applied only when the printer is allowed
// to emit ANSI escapes.
type Cell struct {
	Text  string
	Color Color
}

// Text returns an uncolored cell.
func Text(s string) Cell { return Cell{Text: s} }

// Styled returns a colored cell.
func Styled(c Color, s string) Cell { return Cell{Text: s, Color: c} }

// Table is a terminal table. The same rows are printed as TSV when stdout is
// not a terminal, which is gh's machine-readable convention.
type Table struct {
	Headers []string
	Rows    [][]Cell
}

// TableOptions controls table rendering.
type TableOptions struct {
	// TTY selects aligned, truncated, possibly colored output. Otherwise rows
	// are printed as tab-separated values without a header.
	TTY bool
	// Width is the terminal width; values below 1 mean 80.
	Width int
	// Color enables ANSI colors in TTY mode.
	Color bool
}

// Print renders the table.
func (t *Table) Print(w io.Writer, o TableOptions) error {
	if len(t.Rows) == 0 {
		return nil
	}
	if !o.TTY {
		return t.printTSV(w)
	}
	if o.Width < 1 {
		o.Width = 80
	}

	cols := len(t.Headers)
	for _, row := range t.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	widths := make([]int, cols)
	header := make([]Cell, cols)
	for i, h := range t.Headers {
		header[i] = Text(h)
	}
	rows := make([][]Cell, len(t.Rows))
	for i, row := range t.Rows {
		cells := make([]Cell, cols)
		copy(cells, row)
		for j := range cells {
			cells[j].Text = oneLine(cells[j].Text)
		}
		rows[i] = cells
	}
	for i := 0; i < cols; i++ {
		widths[i] = DisplayWidth(header[i].Text)
		for _, row := range rows {
			if w := DisplayWidth(row[i].Text); w > widths[i] {
				widths[i] = w
			}
		}
	}
	fitWidths(widths, o.Width)

	writeRow := func(cells []Cell) error {
		var b strings.Builder
		for i := 0; i < cols; i++ {
			if i > 0 {
				b.WriteString("  ")
			}
			text := truncateDisplay(cells[i].Text, widths[i])
			b.WriteString(renderCell(Cell{Text: text, Color: cells[i].Color}, o.Color))
			if pad := widths[i] - DisplayWidth(text); pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		_, err := fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
		return err
	}

	if len(t.Headers) > 0 {
		if err := writeRow(header); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) printTSV(w io.Writer) error {
	for _, row := range t.Rows {
		var b strings.Builder
		for i, cell := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(oneLine(cell.Text))
		}
		if _, err := fmt.Fprintln(w, b.String()); err != nil {
			return err
		}
	}
	return nil
}

// fitWidths shrinks the widest columns until the row fits in avail. Columns
// never fall below their header width (capped at 8) until every column has
// reached that floor; only then are they reduced further, from the right.
func fitWidths(widths []int, avail int) {
	if len(widths) == 0 {
		return
	}
	total := 2 * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}
	if total <= avail {
		return
	}
	floors := make([]int, len(widths))
	for i, w := range widths {
		f := w
		if f > 8 {
			f = 8
		}
		if f < 1 {
			f = 1
		}
		floors[i] = f
	}
	excess := total - avail
	for excess > 0 {
		idx := -1
		for i, w := range widths {
			if w <= floors[i] {
				continue
			}
			if idx < 0 || w > widths[idx] {
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		widths[idx]--
		excess--
	}
	for excess > 0 {
		idx := -1
		for i := len(widths) - 1; i >= 0; i-- {
			if widths[i] > 1 {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		widths[idx]--
		excess--
	}
}

// DisplayWidth returns the number of terminal cells s occupies. It is
// Unicode-aware: wide (CJK, emoji) runes count two, combining and format
// runes count zero (BACKLOG B-41).
func DisplayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch r {
	case 0, '\u200b', '\u200c', '\u200d', '\ufe0e', '\ufe0f':
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// truncateDisplay cuts s to at most max display cells, ending with an ellipsis
// instead of splitting a rune or a combining sequence.
func truncateDisplay(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if DisplayWidth(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > max-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return strings.TrimRight(b.String(), " ") + "…"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func renderCell(c Cell, color bool) string {
	if !color || c.Color == ColorNone {
		return c.Text
	}
	if code := ansiCode(c.Color); code != "" {
		return code + c.Text + "\x1b[0m"
	}
	return c.Text
}

func ansiCode(c Color) string {
	switch c {
	case ColorRed:
		return "\x1b[31m"
	case ColorGreen:
		return "\x1b[32m"
	case ColorYellow:
		return "\x1b[33m"
	case ColorBlue:
		return "\x1b[34m"
	case ColorMagenta:
		return "\x1b[35m"
	case ColorCyan:
		return "\x1b[36m"
	case ColorGray:
		return "\x1b[90m"
	case ColorBold:
		return "\x1b[1m"
	}
	return ""
}

// Colorize applies a gh-style color specification ("red", "green+bold") to
// text. It returns text unchanged when colors are disabled.
func Colorize(spec, text string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	var codes strings.Builder
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool { return r == '+' || r == ',' }) {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "red":
			codes.WriteString("\x1b[31m")
		case "green":
			codes.WriteString("\x1b[32m")
		case "yellow":
			codes.WriteString("\x1b[33m")
		case "blue":
			codes.WriteString("\x1b[34m")
		case "magenta":
			codes.WriteString("\x1b[35m")
		case "cyan":
			codes.WriteString("\x1b[36m")
		case "gray", "grey":
			codes.WriteString("\x1b[90m")
		case "bold":
			codes.WriteString("\x1b[1m")
		case "dim":
			codes.WriteString("\x1b[2m")
		case "", "default", "none":
		}
	}
	if codes.Len() == 0 {
		return text
	}
	return codes.String() + text + "\x1b[0m"
}

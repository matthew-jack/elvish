package edit

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xiaq/elvish/edit/tty"
	"github.com/xiaq/elvish/util"
)

// cell is an indivisible unit on the screen. It is not necessarily 1 column
// wide.
type cell struct {
	rune
	width byte
	attr  string
}

// pos is the position within a buffer.
type pos struct {
	line, col int
}

// buffer reflects a continuous range of lines on the terminal. The Unix
// terminal API provides only awkward ways of querying the terminal buffer, so
// we keep an internal reflection and do one-way synchronizations (buffer ->
// terminal, and not the other way around). This requires us to exactly match
// the terminal's idea of the width of characters (wcwidth) and where to
// insert soft carriage returns, so there could be bugs.
type buffer struct {
	width, col, indent int
	newlineWhenFull    bool
	cells              [][]cell // cells reflect len(cells) lines on the terminal.
	dot                pos      // dot is what the user perceives as the cursor.
}

func newBuffer(width int) *buffer {
	return &buffer{width: width, cells: [][]cell{make([]cell, 0, width)}}
}

func (b *buffer) appendCell(c cell) {
	n := len(b.cells)
	b.cells[n-1] = append(b.cells[n-1], c)
	b.col += int(c.width)
}

func (b *buffer) appendLine() {
	b.cells = append(b.cells, make([]cell, 0, b.width))
	b.col = 0
}

func (b *buffer) newline() {
	b.appendLine()

	if b.indent > 0 {
		for i := 0; i < b.indent; i++ {
			b.appendCell(cell{rune: ' ', width: 1})
		}
	}
}

func (b *buffer) extend(b2 *buffer) {
	if b2 != nil && b2.cells != nil {
		b.cells = append(b.cells, b2.cells...)
		b.col = b2.col
	}
}

// write appends a single rune to a buffer.
func (b *buffer) write(r rune, attr string) {
	if r == '\n' {
		b.newline()
		return
	} else if !unicode.IsPrint(r) {
		// BUG(xiaq): buffer.write drops unprintable runes silently
		return
	}
	wd := wcwidth(r)
	c := cell{r, byte(wd), attr}

	if b.col+wd > b.width {
		b.newline()
		b.appendCell(c)
	} else {
		b.appendCell(c)
		if b.col == b.width && b.newlineWhenFull {
			b.newline()
		}
	}
}

func (b *buffer) writes(s string, attr string) {
	for _, r := range s {
		b.write(r, attr)
	}
}

func (b *buffer) writePadding(w int, attr string) {
	b.writes(strings.Repeat(" ", w), attr)
}

func (b *buffer) line() int {
	return len(b.cells) - 1
}

func (b *buffer) cursor() pos {
	return pos{len(b.cells) - 1, b.col}
}

func (b *buffer) trimToLines(low, high int) {
	for i := 0; i < low; i++ {
		b.cells[i] = nil
	}
	for i := high; i < len(b.cells); i++ {
		b.cells[i] = nil
	}
	b.cells = b.cells[low:high]
	b.dot.line -= low
}

// writer is the part of an Editor responsible for keeping the status of and
// updating the screen.
type writer struct {
	file   *os.File
	oldBuf *buffer
}

func newWriter(f *os.File) *writer {
	writer := &writer{file: f, oldBuf: newBuffer(0)}
	return writer
}

// deltaPos calculates the escape sequence needed to move the cursor from one
// position to another.
func deltaPos(from, to pos) []byte {
	buf := new(bytes.Buffer)
	if from.line < to.line {
		// move down
		fmt.Fprintf(buf, "\033[%dB", to.line-from.line)
	} else if from.line > to.line {
		// move up
		fmt.Fprintf(buf, "\033[%dA", from.line-to.line)
	}
	fmt.Fprintf(buf, "\033[%dG", to.col+1)
	return buf.Bytes()
}

// commitBuffer updates the terminal display to reflect current buffer.
// TODO Instead of erasing w.oldBuf entirely and then draw buf, compute a
// delta between w.oldBuf and buf
func (w *writer) commitBuffer(buf *buffer) error {
	bytesBuf := new(bytes.Buffer)

	pLine := w.oldBuf.dot.line
	if pLine > 0 {
		fmt.Fprintf(bytesBuf, "\033[%dA", pLine)
	}
	bytesBuf.WriteString("\r\033[J")

	attr := ""
	for i, line := range buf.cells {
		if i > 0 {
			bytesBuf.WriteString("\n")
		}
		for _, c := range line {
			if c.width > 0 && c.attr != attr {
				fmt.Fprintf(bytesBuf, "\033[m\033[%sm", c.attr)
				attr = c.attr
			}
			bytesBuf.WriteString(string(c.rune))
		}
	}
	if attr != "" {
		bytesBuf.WriteString("\033[m")
	}
	cursor := buf.cursor()
	if cursor.col == buf.width {
		cursor.col--
	}
	bytesBuf.Write(deltaPos(cursor, buf.dot))

	_, err := w.file.Write(bytesBuf.Bytes())
	if err != nil {
		return err
	}

	w.oldBuf = buf
	return nil
}

func lines(bufs ...*buffer) (l int) {
	for _, buf := range bufs {
		if buf != nil {
			l += len(buf.cells)
		}
	}
	return
}

// findWindow finds a window of lines around the selected line in a total
// number of height lines, that is at most max lines.
func findWindow(height, selected, max int) (low, high int) {
	if height > max {
		low = selected - max/2
		high = low + max
		switch {
		case low < 0:
			// Near top of the list, move the window down
			low = 0
			high = low + max
		case high > height:
			// Near bottom of the list, move the window down
			high = height
			low = high - max
		}
		return
	} else {
		return 0, height
	}
}

func trimToWindow(s []string, selected, max int) ([]string, int) {
	low, high := findWindow(len(s), selected, max)
	return s[low:high], low
}

// refresh redraws the line editor. The dot is passed as an index into text;
// the corresponding position will be calculated.
func (w *writer) refresh(bs *editorState) error {
	winsize := tty.GetWinsize(int(w.file.Fd()))
	width, height := int(winsize.Col), int(winsize.Row)

	var bufLine, bufMode, bufTips, bufListing, buf *buffer
	// bufLine
	b := newBuffer(width)
	bufLine = b

	b.newlineWhenFull = true

	b.writes(bs.prompt, attrForPrompt)

	if b.line() == 0 && b.col*2 < b.width {
		b.indent = b.col
	}

	// i keeps track of number of bytes written.
	i := 0
	if bs.dot == 0 {
		b.dot = b.cursor()
	}

	comp := bs.completion
	var suppress = false

tokens:
	for _, token := range bs.tokens {
		for _, r := range token.Val {
			if suppress && i < comp.end {
				// Silence the part that is being completed
			} else {
				b.write(r, attrForType[token.Typ])
			}
			i += utf8.RuneLen(r)
			if comp != nil && comp.current != -1 && i == comp.start {
				// Put the current candidate and instruct text up to comp.end
				// to be suppressed. The cursor should be placed correctly
				// (i.e. right after the candidate)
				for _, part := range comp.candidates[comp.current].parts {
					attr := attrForType[comp.typ]
					if part.completed {
						attr += attrForCompleted
					}
					b.writes(part.text, attr)
				}
				suppress = true
			}
			if bs.mode == modeHistory && i == len(bs.history.prefix) {
				break tokens
			}
			if bs.dot == i {
				b.dot = b.cursor()
			}
		}
	}

	if bs.mode == modeHistory {
		// Put the rest of current history, position the cursor at the
		// end of the line, and finish writing
		h := bs.history
		b.writes(h.items[h.current][len(h.prefix):], attrForCompletedHistory)
		b.dot = b.cursor()
	}

	// Write rprompt
	padding := b.width - b.col - wcwidths(bs.rprompt)
	if padding >= 1 {
		b.newlineWhenFull = false
		b.writePadding(padding, "")
		b.writes(bs.rprompt, attrForRprompt)
	}

	// bufMode
	if bs.mode != modeInsert {
		b := newBuffer(width)
		bufMode = b
		text := ""
		switch bs.mode {
		case modeCommand:
			text = "Command"
			b.writes(trimWcwidth("Command", width), attrForMode)
		case modeCompletion:
			text = fmt.Sprintf("Completing %s", bs.line[comp.start:comp.end])
		case modeNavigation:
			text = "Navigating"
		case modeHistory:
			text = fmt.Sprintf("History #%d", bs.history.current)
		}
		b.writes(trimWcwidth(text, width), attrForMode)
	}

	// bufTips
	// TODO tips is assumed to contain no newlines.
	if len(bs.tips) > 0 {
		b := newBuffer(width)
		bufTips = b
		b.writes(trimWcwidth(strings.Join(bs.tips, ", "), width), attrForTip)
	}

	listingHeight := 0
	// Trim lines and determine the maximum height for bufListing
	switch {
	case height >= lines(bufLine, bufMode, bufTips):
		listingHeight = height - lines(bufLine, bufMode, bufTips)
	case height >= lines(bufLine, bufTips):
		bufMode, bufListing = nil, nil
	case height >= lines(bufLine):
		bufTips, bufMode, bufListing = nil, nil, nil
	case height >= 1:
		bufTips, bufMode, bufListing = nil, nil, nil
		dotLine := bufLine.dot.line
		bufLine.trimToLines(dotLine+1-height, dotLine+1)
	default:
		bufLine, bufTips, bufMode, bufListing = nil, nil, nil, nil
	}

	// Render bufListing under the maximum height constraint
	nav := bs.navigation
	if listingHeight > 0 && comp != nil || nav != nil {
		b := newBuffer(width)
		bufListing = b
		// Completion listing
		if comp != nil {
			// Layout candidates in multiple columns
			cands := comp.candidates

			// First decide the shape (# of rows and columns)
			colWidth := 0
			colMargin := 2
			for _, cand := range cands {
				width := wcwidths(cand.text)
				if colWidth < width {
					colWidth = width
				}
			}

			cols := (b.width + colMargin) / (colWidth + colMargin)
			if cols == 0 {
				cols = 1
			}
			lines := util.CeilDiv(len(cands), cols)
			bs.completionLines = lines

			// Determine the window to show.
			low, high := findWindow(lines, comp.current%lines, listingHeight)
			for i := low; i < high; i++ {
				if i > low {
					b.newline()
				}
				for j := 0; j < cols; j++ {
					k := j*lines + i
					if k >= len(cands) {
						continue
					}
					attr := ""
					if k == comp.current {
						attr = attrForCurrentCompletion
					}
					text := cands[k].text
					b.writes(text, attr)
					b.writePadding(colWidth-wcwidths(text), attr)
					b.writePadding(colMargin, "")
				}
			}
		}

		// Navigation listing
		if nav != nil {
			b := newBuffer(width)
			bufListing = b

			filenames, low := trimToWindow(nav.current.names, nav.current.selected, listingHeight)
			parentFilenames, parentLow := trimToWindow(nav.parent.names, nav.parent.selected, listingHeight)

			// TODO(xiaq): When laying out the navigation listing, determine
			// the width of two columns more intelligently instead of
			// allocating half of screen for each. Maybe the algorithm used by
			// ranger could be pirated.
			colMargin := 1
			parentWidth := (width + colMargin) / 2
			currentWidth := width - colMargin - parentWidth
			for i := 0; i < len(filenames) || i < len(parentFilenames); i++ {
				if i > 0 {
					b.newline()
				}
				text, attr := "", ""
				if i < len(parentFilenames) {
					text = parentFilenames[i]
				}
				if i+parentLow == nav.parent.selected {
					attr = attrForSelectedFile
				}
				b.writes(trimWcwidth(text, parentWidth), attr)
				b.writePadding(parentWidth-wcwidths(text), attr)
				b.writePadding(colMargin, "")

				if i < len(filenames) {
					attr := ""
					if i+low == nav.current.selected {
						attr = attrForSelectedFile
					}
					text := filenames[i]
					b.writes(trimWcwidth(text, currentWidth), attr)
					b.writePadding(currentWidth-wcwidths(text), attr)
				}
			}
		}
		// Trim bufListing.
		// XXX This algorithm only works for completion listing.

	}

	// Combine buffers (reusing bufLine)
	buf = bufLine
	buf.extend(bufMode)
	buf.extend(bufTips)
	buf.extend(bufListing)

	return w.commitBuffer(buf)
}

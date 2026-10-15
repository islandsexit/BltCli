package repository

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/utils"
)

// DiffLine is one aligned, bounded row in a complete value comparison.
type DiffLine struct {
	Base, Other         string
	BaseLine, OtherLine int64
	Kind                string
}

// DiffDocument owns temporary row, offset and change-block files, without open
// handles. Memory is bounded by a 64-row lookahead and 4 KiB text segments.
type DiffDocument struct {
	Path                string
	Count, Changes      int64
	BaseJSON, OtherJSON bool
	Formatted           bool
	BaseHex, OtherHex   bool
}

type documentLine struct {
	text   string
	number int64
}
type documentLines struct {
	reader                                              *bufio.Reader
	json, hex, present, quoted, escaped, ended, emitted bool
	fragmented                                          bool
	depth                                               int
	previous                                            byte
	pending                                             string
	byteValues                                          string
	line, offset                                        int64
}

func newDocumentLines(r io.Reader, present bool) (*documentLines, error) {
	d := &documentLines{reader: bufio.NewReaderSize(r, 64<<10), present: present, line: 1}
	var table [256]byte
	for i := range table {
		table[i] = byte(i)
	}
	d.byteValues = string(table[:])
	probe, err := d.reader.Peek(4096)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	valid := utf8.Valid(probe)
	if !valid && len(probe) == 4096 {
		for cut := 1; cut <= 3; cut++ {
			if utf8.Valid(probe[:len(probe)-cut]) {
				valid = true
				break
			}
		}
	}
	if !valid {
		d.hex = true
	}
	for _, c := range probe {
		if c < 32 && c != '\n' && c != '\r' && c != '\t' {
			d.hex = true
			break
		}
	}
	if valid {
		for _, r := range string(probe) {
			if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				d.hex = true
				break
			}
		}
	}
	if !d.hex {
		trim := bytes.TrimSpace(probe)
		d.json = len(trim) > 0 && (trim[0] == '{' || trim[0] == '[') || json.Valid(probe)
	}
	return d, nil
}

func (d *documentLines) indent() string { return strings.Repeat("  ", min(64, max(0, d.depth))) }
func (d *documentLines) token(ctx context.Context) error {
	c, err := d.reader.ReadByte()
	if err != nil {
		return err
	}
	if !d.json {
		switch c {
		case '\n':
			d.pending = "\n"
		case '\t':
			d.pending = "\\t"
		case '\r':
			d.pending = "\\r"
		default:
			if c < 32 || c == 127 {
				d.pending = fmt.Sprintf("\\x%02x", c)
			} else {
				d.pending = d.byteValues[int(c) : int(c)+1]
			}
		}
		return nil
	}
	if d.quoted {
		d.pending = d.byteValues[int(c) : int(c)+1]
		if d.escaped {
			d.escaped = false
		} else if c == '\\' {
			d.escaped = true
		} else if c == '"' {
			d.quoted = false
		}
		return nil
	}
	switch c {
	case ' ', '\t', '\r', '\n':
		return nil
	case '"':
		d.quoted = true
		d.pending = "\""
	case '{', '[':
		d.depth++
		d.pending = string(c)
		for spaces := 0; ; spaces++ {
			if spaces%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			p, e := d.reader.Peek(1)
			if e != nil {
				break
			}
			if !bytes.ContainsRune([]byte(" \r\n\t"), rune(p[0])) {
				if p[0] != '}' && p[0] != ']' {
					d.pending += "\n" + d.indent()
				}
				break
			}
			if _, err := d.reader.ReadByte(); err != nil {
				return err
			}
		}
	case '}', ']':
		d.depth--
		if d.previous != '{' && d.previous != '[' {
			d.pending = "\n" + d.indent()
		}
		d.pending += string(c)
	case ',':
		d.pending = ",\n" + d.indent()
	case ':':
		d.pending = ": "
	default:
		if c < 32 || c == 127 {
			d.pending = fmt.Sprintf("\\x%02x", c)
		} else {
			d.pending = d.byteValues[int(c) : int(c)+1]
		}
	}
	d.previous = c
	return nil
}

func documentText(s string) string {
	if !utf8.ValidString(s) {
		return utils.Display([]byte(s), len(s))
	}
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	var out strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			fmt.Fprintf(&out, "\\u%04x", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (d *documentLines) next(ctx context.Context) (documentLine, error) {
	if !d.present {
		return documentLine{}, io.EOF
	}
	if d.hex {
		if err := ctx.Err(); err != nil {
			return documentLine{}, err
		}
		var buf [16]byte
		n, err := io.ReadFull(d.reader, buf[:])
		if n == 0 {
			return documentLine{}, err
		}
		text := fmt.Sprintf("%08x  %-47s  %s", d.offset, fmt.Sprintf("% x", buf[:n]), utils.SafeText(string(bytes.Map(func(r rune) rune {
			if r < 32 || r > 126 {
				return '.'
			}
			return r
		}, buf[:n]))))
		d.offset += int64(n)
		line := documentLine{text, d.line}
		d.line++
		return line, nil
	}
	var out strings.Builder
	for steps := 0; ; steps++ {
		if steps%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return documentLine{}, err
			}
		}
		if d.pending != "" {
			c := d.pending[0]
			d.pending = d.pending[1:]
			if c == '\n' {
				if out.Len() == 0 && d.fragmented {
					d.fragmented = false
					d.line++
					continue
				}
				d.fragmented = false
				line := documentLine{documentText(out.String()), d.line}
				d.line++
				d.emitted = true
				return line, nil
			}
			out.WriteByte(c)
			if out.Len() >= 4096 && utf8.ValidString(out.String()) {
				d.emitted = true
				d.fragmented = true
				return documentLine{documentText(out.String()), d.line}, nil
			}
			// Invalid UTF-8 cannot keep a segment growing without a bound.
			if out.Len() >= 4100 {
				d.emitted = true
				d.fragmented = true
				return documentLine{utils.Display([]byte(out.String()), out.Len()), d.line}, nil
			}
			continue
		}
		if d.ended {
			if out.Len() == 0 && d.emitted {
				return documentLine{}, io.EOF
			}
			d.emitted = true
			return documentLine{documentText(out.String()), d.line}, nil
		}
		if err := d.token(ctx); err != nil {
			if errors.Is(err, io.EOF) {
				d.ended = true
			} else {
				return documentLine{}, err
			}
		}
	}
}

// BuildDiffDocument streams complete snapshot values to an aligned disk view.
// Local matching uses a bounded lookahead, not an unbounded edit-distance matrix.
func (s *DiffStore) BuildDiffDocument(ctx context.Context, dir string, e domain.DiffEntry) (doc *DiffDocument, err error) {
	doc, err = s.BuildDiffDocumentMode(ctx, dir, e, true)
	if err == nil && doc.Changes == 0 && e.Kind == "record" {
		if err = doc.Remove(); err != nil {
			return nil, err
		}
		doc, err = s.BuildDiffDocumentMode(ctx, dir, e, false)
		if err == nil && doc.Changes == 0 {
			if err = doc.Remove(); err != nil {
				return nil, err
			}
			return s.buildDiffDocument(ctx, dir, e, false, true)
		}
	}
	return doc, err
}

// BuildDiffDocumentMode selects a formatted JSON or raw text representation.
func (s *DiffStore) BuildDiffDocumentMode(ctx context.Context, dir string, e domain.DiffEntry, format bool) (doc *DiffDocument, err error) {
	return s.buildDiffDocument(ctx, dir, e, format, false)
}

// BuildDiffDocumentHex displays every byte explicitly on both sides.
func (s *DiffStore) BuildDiffDocumentHex(ctx context.Context, dir string, e domain.DiffEntry) (*DiffDocument, error) {
	return s.buildDiffDocument(ctx, dir, e, false, true)
}

func (s *DiffStore) buildDiffDocument(ctx context.Context, dir string, e domain.DiffEntry, format, forceHex bool) (doc *DiffDocument, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := os.Open(s.Index + ".values")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, snapshot.Close()) }()
	stat, err := snapshot.Stat()
	if err != nil {
		return nil, err
	}
	side := func(base bool) (*documentLines, error) {
		present, bucket, size, offset, seq := e.OtherPresent, e.OtherBucket, e.OtherBytes, e.OtherOffset, e.OtherSequence
		if base {
			present, bucket, size, offset, seq = e.BasePresent, e.BaseBucket, e.BaseBytes, e.BaseOffset, e.BaseSequence
		}
		if e.Kind == "bucket" || bucket {
			return newDocumentLines(strings.NewReader(fmt.Sprintf("<bucket · sequence %d>", seq)), present)
		}
		if size < 0 || offset < 0 || offset > stat.Size() || int64(size) > stat.Size()-offset {
			return nil, errors.New("invalid value snapshot bounds")
		}
		return newDocumentLines(io.NewSectionReader(snapshot, offset, int64(size)), present)
	}
	left, err := side(true)
	if err != nil {
		return nil, err
	}
	right, err := side(false)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, "diff-document-*")
	if err != nil {
		return nil, err
	}
	if forceHex {
		left.hex = true
		right.hex = true
		left.json = false
		right.json = false
	}
	doc = &DiffDocument{Path: file.Name(), BaseJSON: left.json, OtherJSON: right.json, Formatted: format, BaseHex: left.hex, OtherHex: right.hex}
	if !format {
		left.json = false
		right.json = false
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, doc.Remove())
			doc = nil
		}
	}()
	index, err := os.OpenFile(doc.Path+".index", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return doc, err
	}
	defer func() { err = errors.Join(err, index.Close()) }()
	changes, err := os.OpenFile(doc.Path+".changes", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return doc, err
	}
	defer func() { err = errors.Join(err, changes.Close()) }()
	dataWriter, indexWriter, changeWriter := bufio.NewWriterSize(file, 64<<10), bufio.NewWriterSize(index, 64<<10), bufio.NewWriterSize(changes, 64<<10)
	counted := &countedWriter{w: dataWriter}
	encoder := json.NewEncoder(counted)
	wasChanged := false
	emit := func(a, b documentLine) error {
		row := DiffLine{Base: a.text, Other: b.text, BaseLine: a.number, OtherLine: b.number, Kind: "equal"}
		if a.number == 0 {
			row.Kind = "insert"
		} else if b.number == 0 {
			row.Kind = "delete"
		} else if a.text != b.text {
			row.Kind = "replace"
		}
		var token [8]byte
		binary.LittleEndian.PutUint64(token[:], counted.n)
		if _, err := indexWriter.Write(token[:]); err != nil {
			return err
		}
		if row.Kind != "equal" && !wasChanged {
			binary.LittleEndian.PutUint64(token[:], uint64(doc.Count))
			if _, err := changeWriter.Write(token[:]); err != nil {
				return err
			}
			doc.Changes++
		}
		wasChanged = row.Kind != "equal"
		if err := encoder.Encode(row); err != nil {
			return err
		}
		doc.Count++
		return nil
	}
	fill := func(lines []documentLine, source *documentLines) ([]documentLine, error) {
		for len(lines) < 64 {
			line, e := source.next(ctx)
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				return nil, e
			}
			lines = append(lines, line)
		}
		return lines, nil
	}
	var a, b []documentLine
	for {
		if err = ctx.Err(); err != nil {
			return doc, err
		}
		a, err = fill(a, left)
		if err != nil {
			return doc, err
		}
		b, err = fill(b, right)
		if err != nil {
			return doc, err
		}
		if len(a) == 0 && len(b) == 0 {
			break
		}
		if len(a) > 0 && len(b) > 0 && a[0].text == b[0].text {
			if err = emit(a[0], b[0]); err != nil {
				return doc, err
			}
			a = a[1:]
			b = b[1:]
			continue
		}
		ai, bi := len(a), len(b)
		cost := ai + bi + 1
		anchors := make(map[string]int, len(a))
		for i, line := range a {
			if _, ok := anchors[line.text]; !ok {
				anchors[line.text] = i
			}
		}
		for j, line := range b {
			if i, ok := anchors[line.text]; ok && i+j < cost {
				ai, bi, cost = i, j, i+j
			}
		}
		for i := 0; i < max(ai, bi); i++ {
			var av, bv documentLine
			if i < ai {
				av = a[i]
			}
			if i < bi {
				bv = b[i]
			}
			if err = emit(av, bv); err != nil {
				return doc, err
			}
		}
		a = a[ai:]
		b = b[bi:]
	}
	err = errors.Join(dataWriter.Flush(), indexWriter.Flush(), changeWriter.Flush(), ctx.Err())
	return doc, err
}

// Page seeks directly to at most limit aligned rows.
func (d *DiffDocument) Page(ctx context.Context, start int64, limit int) (rows []DiffLine, err error) {
	if start < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("invalid document page")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if start >= d.Count {
		return nil, nil
	}
	index, err := os.Open(d.Path + ".index")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, index.Close()) }()
	var offset [8]byte
	if _, err = index.ReadAt(offset[:], start*8); err != nil {
		return nil, err
	}
	file, err := os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err = file.Seek(int64(binary.LittleEndian.Uint64(offset[:])), io.SeekStart); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 64<<10))
	for i := 0; i < limit && start+int64(i) < d.Count; i++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var row DiffLine
		if err = decoder.Decode(&row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Change seeks the preceding/following change block without scanning value rows.
func (d *DiffDocument) Change(ctx context.Context, row int64, next bool) (position int64, err error) {
	if err = ctx.Err(); err != nil {
		return row, err
	}
	if d.Changes == 0 {
		return row, nil
	}
	file, err := os.Open(d.Path + ".changes")
	if err != nil {
		return row, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	read := func(i int64) (int64, error) {
		var token [8]byte
		_, e := file.ReadAt(token[:], i*8)
		return int64(binary.LittleEndian.Uint64(token[:])), e
	}
	low, high := int64(0), d.Changes
	for low < high {
		if err = ctx.Err(); err != nil {
			return row, err
		}
		mid := (low + high) / 2
		v, e := read(mid)
		if e != nil {
			return row, e
		}
		if v < row || next && v == row {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if next {
		if low >= d.Changes {
			return row, nil
		}
		return read(low)
	}
	if low == 0 {
		return row, nil
	}
	return read(low - 1)
}

// Remove releases all temporary document files; repeated calls are safe.
func (d *DiffDocument) Remove() error {
	remove := func(path string) error {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return errors.Join(remove(d.Path), remove(d.Path+".index"), remove(d.Path+".changes"))
}

package modelstep

import (
	"strings"

	"github.com/corygyarmathy/slipbox/internal/note"
)

// File is one `=== FILE: <name> ===` block of a reply, one
// `=== EDIT: <name> ===` block, or one `=== DELETE: <name> ===` line.
type File struct {
	Name string
	Body string
	// Delete is set for a DELETE line, which has no body: the repair pass's
	// way to delete an aside note.
	Delete bool
	// Edit is set for an EDIT block, whose body is edits to the named note
	// (note.ParseEdits) rather than the whole note.
	Edit bool
}

const (
	fileMarker   = "=== FILE:"
	deleteMarker = "=== DELETE:"
	editMarker   = "=== EDIT:"
)

// Split parses a reply into its `=== FILE: <name> ===` blocks and the
// trailing `=== REPORT ===` or `=== MANIFEST ===` block. Markers may be
// wrapped in backticks, so one set as code still counts. Each block is
// trimmed and given one trailing newline. Text before the first marker is the
// model's preamble, not a note, and is dropped. A repeated name keeps its
// first place with the last body, as a model that writes a note again means
// the later version. A block with an empty name names no note, and is
// dropped. Lines end where the note model ends them (note.Parse), so a reply
// and the notes cut from it agree on where a line ends.
//
// A `=== DELETE: <name> ===` line is a marker too: it ends the block before
// it and gives a File with Delete set. Text after it, up to the next marker,
// is dropped, since a deletion has no body. A DELETE and a FILE of the same
// name are a repeated name, so the later one wins.
func Split(text string) (files []File, tail string) {
	const tailName = "\x00tail"
	var (
		cur      string
		started  bool
		deleting bool
		editing  bool
		buf      []string
		index    = map[string]int{}
	)
	flush := func() {
		// A block with an empty name names no note.
		if !started || cur == "" {
			return
		}
		key := cur
		f := File{Name: cur, Body: strings.TrimSpace(strings.Join(buf, "\n")) + "\n"}
		switch {
		case deleting:
			f = File{Name: cur, Delete: true}
		case editing:
			key = "\x00edit " + cur
			f = File{Name: cur, Body: strings.Trim(strings.Join(buf, "\n"), "\n") + "\n", Edit: true}
			if i, ok := index[key]; ok {
				files[i].Body += f.Body
				return
			}
		}
		if i, ok := index[key]; ok {
			files[i] = f
			return
		}
		index[key] = len(files)
		files = append(files, f)
	}
	for _, l := range note.Parse("", text).Lines {
		line := l.Text
		s := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "`"))
		switch {
		case strings.HasPrefix(s, fileMarker) && strings.HasSuffix(s, "==="):
			flush()
			cur, started, deleting, editing, buf = strings.TrimSpace(s[len(fileMarker):len(s)-3]), true, false, false, nil
		case strings.HasPrefix(s, deleteMarker) && strings.HasSuffix(s, "==="):
			flush()
			cur, started, deleting, editing, buf = strings.TrimSpace(s[len(deleteMarker):len(s)-3]), true, true, false, nil
		case strings.HasPrefix(s, editMarker) && strings.HasSuffix(s, "==="):
			flush()
			cur, started, deleting, editing, buf = strings.TrimSpace(s[len(editMarker):len(s)-3]), true, false, true, nil
		case s == "=== REPORT ===" || s == "=== MANIFEST ===":
			flush()
			cur, started, deleting, editing, buf = tailName, true, false, false, nil
		default:
			buf = append(buf, line)
		}
	}
	flush()
	if i, ok := index[tailName]; ok {
		tail = files[i].Body
		files = append(files[:i], files[i+1:]...)
	}
	return files, tail
}

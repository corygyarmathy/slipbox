// Package fallback records the segments a stage 2 or 3 step failed, each of
// which fell back instead of stopping the run (ADR 0009): it goes on with the notes the stage before gave it. A segment
// fails step 2.1 or 3.1 with a reply that can't be written, and step 2.1's
// gates or step 3.1's with a note that fails one. Each of the four steps
// writes its segments into its own directory, as run.FallbacksFile, and the
// run report reads them back (ADR 0002 §5: the run folder is the only channel
// between steps).
package fallback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/corygyarmathy/slipbox/internal/run"
)

// Item is one segment that fell back.
type Item struct {
	// Segment is the segment's number.
	Segment int `json:"segment"`
	// Why is the reason: the reply's fault, or the gate the segment failed.
	Why string `json:"why"`
	// Report is the file, in the step's directory, of the report of the gate
	// the segment failed, or "" for a reply's fault.
	Report string `json:"report,omitempty"`
}

// Write writes items into step's directory as run.FallbacksFile, an empty
// list if there are none, so a complete step says no segment fell back.
func Write(step *run.Step, items []Item) error {
	if items == nil {
		items = []Item{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(items); err != nil {
		return err
	}
	return step.WriteFile(run.FallbacksFile, b.Bytes())
}

// Read reads the segments that fell back in the step in dir, in folder and
// slash-separated. A step with no run.FallbacksFile had none.
func Read(folder *run.Folder, dir string) ([]Item, error) {
	p := filepath.Join(folder.Dir, filepath.FromSlash(dir), run.FallbacksFile)
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return items, nil
}

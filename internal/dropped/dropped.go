// Package dropped records what a stage 1 step dropped instead of stopping the
// run (ADR 0009): a reply's file or edit that can't be
// applied, a proposer's item that cites no line of the uncovered list (a
// remark), a kept loss the call added no statement for, or a broken or
// untraced statement. Each step that drops anything writes its items into
// its own directory, as run.DroppedFile, and the run report reads them back
// (ADR 0002 §5: the run folder is the only channel between steps).
package dropped

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

// Item is one thing a step dropped.
type Item struct {
	// Segment is the item's segment number.
	Segment int `json:"segment"`
	// Note is the note's file name the item was in or for, or "" if it
	// names none.
	Note string `json:"note,omitempty"`
	// What is what was dropped, such as an edit's statement or a file's name.
	What string `json:"what"`
	// Why is the reason, such as the error that would have failed the step.
	Why string `json:"why"`
	// Loss is the kept loss's ID (keptloss.Kept.ID), for a kept loss the
	// kept-loss call added no statement for, or "".
	Loss string `json:"loss,omitempty"`
}

// Write writes items into step's directory as run.DroppedFile, an empty list
// if there are none, so a complete step says it dropped nothing.
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
	return step.WriteFile(run.DroppedFile, b.Bytes())
}

// Read reads the items the step in dir, in folder and slash-separated,
// dropped. A step with no run.DroppedFile dropped nothing.
func Read(folder *run.Folder, dir string) ([]Item, error) {
	p := filepath.Join(folder.Dir, filepath.FromSlash(dir), run.DroppedFile)
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

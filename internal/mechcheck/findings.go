package mechcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corygyarmathy/slipbox/internal/run"
)

// kindNames are the kinds' names in run.FindingsFile, by Kind.
var kindNames = [numKinds]string{
	NoSegment:       "no-segment",
	CrossSegment:    "cross-segment",
	LeadIn:          "lead-in",
	NoTerminal:      "no-terminal",
	LowercaseItem:   "lowercase-item",
	Untraced:        "untraced",
	SectionNotFound: "section-not-found",
	Broken:          "broken",
	Fuzzy:           "fuzzy",
}

// MarshalJSON writes the kind as its name.
func (k Kind) MarshalJSON() ([]byte, error) {
	if k < 0 || int(k) >= len(kindNames) {
		return nil, fmt.Errorf("mechcheck: unknown finding kind %d", k)
	}
	return json.Marshal(kindNames[k])
}

// UnmarshalJSON reads the kind from its name.
func (k *Kind) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	for i, n := range kindNames {
		if n == name {
			*k = Kind(i)
			return nil
		}
	}
	return fmt.Errorf("mechcheck: unknown finding kind %q", name)
}

// findingsJSON renders the report's notes as run.FindingsFile holds them.
func (r *Report) findingsJSON() ([]byte, error) {
	notes := make([]NoteReport, len(r.Notes))
	for i, nr := range r.Notes {
		if nr.Findings == nil {
			nr.Findings = []Finding{}
		}
		notes[i] = nr
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(notes); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ReadFindings reads back the findings step 1.2 wrote into the step
// directory dir, in folder and slash-separated. The report it gives has only
// Notes filled: each note's findings, in the check's order.
func ReadFindings(folder *run.Folder, dir string) (*Report, error) {
	p := filepath.Join(folder.Dir, filepath.FromSlash(dir), run.FindingsFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	r := &Report{}
	if err := json.Unmarshal(b, &r.Notes); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}

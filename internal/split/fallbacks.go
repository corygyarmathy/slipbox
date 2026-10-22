package split

import (
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Fallbacks are the segments of src that fall back at a stage 2 or 3 gate
// step, by segment folder: those its model step listed for a reply it
// couldn't write, then those that fail one of its gates.
type Fallbacks struct {
	src      *source.Source
	byFolder map[string]fallback.Item
}

// NewFallbacks gives src's fallbacks, starting with the model step's, items.
func NewFallbacks(src *source.Source, items []fallback.Item) *Fallbacks {
	fb := &Fallbacks{src: src, byFolder: map[string]fallback.Item{}}
	for _, seg := range src.Segments {
		for _, it := range items {
			if it.Segment == seg.Number {
				fb.byFolder[Folder(seg)] = it
			}
		}
	}
	return fb
}

// Has says whether the segment folder named name falls back.
func (fb *Fallbacks) Has(name string) bool {
	_, ok := fb.byFolder[name]
	return ok
}

// Without is folders less those that fall back, in a new slice.
func (fb *Fallbacks) Without(folders []SegmentFolder) []SegmentFolder {
	var out []SegmentFolder
	for _, f := range folders {
		if !fb.Has(f.Name) {
			out = append(out, f)
		}
	}
	return out
}

// Fail has each of the folders named fall back for failing the gate named
// gate, whose report is report in the step's directory. A folder no segment
// of src claims is left out.
func (fb *Fallbacks) Fail(folders []string, gate, report string) {
	for _, seg := range fb.src.Segments {
		for _, name := range folders {
			if name == Folder(seg) {
				fb.byFolder[name] = fallback.Item{Segment: seg.Number, Why: gate, Report: report}
			}
		}
	}
}

// Items are the fallbacks, in segment order, for fallback.Write.
func (fb *Fallbacks) Items() []fallback.Item {
	var out []fallback.Item
	for _, seg := range fb.src.Segments {
		if it, ok := fb.byFolder[Folder(seg)]; ok {
			out = append(out, it)
		}
	}
	return out
}

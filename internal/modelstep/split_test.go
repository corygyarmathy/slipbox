package modelstep

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		files []File
		tail  string
	}{
		{
			name:  "files and a report",
			reply: "=== FILE: 0 A.md ===\n# 0 A\n\nText.[^1]\n\n=== FILE: 0 A - Aside - B.md ===\n# B\n=== REPORT ===\nRepairs:\n- none\n",
			files: []File{{Name: "0 A.md", Body: "# 0 A\n\nText.[^1]\n"}, {Name: "0 A - Aside - B.md", Body: "# B\n"}},
			tail:  "Repairs:\n- none\n",
		},
		{
			name:  "backticked markers and a manifest",
			reply: "```\n=== FILE: 1 C.md ===\n```\n\nBody\n`=== MANIFEST ===`\n{}\n",
			files: []File{{Name: "1 C.md", Body: "```\n\nBody\n"}},
			tail:  "{}\n",
		},
		{
			name:  "lines end where the note model ends them: \\n, \\r\\n or a lone \\r",
			reply: "=== FILE: A ===\r\na\r=== FILE: B ===\rb\vc\u2028d\r",
			files: []File{{Name: "A", Body: "a\n"}, {Name: "B", Body: "b\vc\u2028d\n"}},
		},
		{
			name:  "preamble before the first marker is dropped",
			reply: "Here are the notes.\n=== FILE: X ===\nx\n",
			files: []File{{Name: "X", Body: "x\n"}},
		},
		{
			name:  "a repeated name keeps its first place and its last body",
			reply: "=== FILE: A ===\none\n=== FILE: B ===\nb\n=== FILE: A ===\ntwo\n",
			files: []File{{Name: "A", Body: "two\n"}, {Name: "B", Body: "b\n"}},
		},
		{
			name:  "an empty name is dropped",
			reply: "=== FILE: ===\nlost\n=== FILE: A ===\na\n",
			files: []File{{Name: "A", Body: "a\n"}},
		},
		{
			name:  "CRLF and surrounding blank lines are trimmed",
			reply: "=== FILE: A ===\r\n\r\n  a\r\nb\r\n\r\n=== REPORT ===\r\nr",
			files: []File{{Name: "A", Body: "a\nb\n"}},
			tail:  "r\n",
		},
		{
			name:  "a DELETE line ends the block before it and drops what follows it",
			reply: "=== FILE: 0 A.md ===\na\n=== DELETE: 0 A - Aside - B.md ===\nstray\n=== FILE: 0 A - Aside - C.md ===\nc\n=== REPORT ===\nr\n",
			files: []File{{Name: "0 A.md", Body: "a\n"}, {Name: "0 A - Aside - B.md", Delete: true}, {Name: "0 A - Aside - C.md", Body: "c\n"}},
			tail:  "r\n",
		},
		{
			name:  "a DELETE and a FILE of the same name: the later wins",
			reply: "=== FILE: A ===\na\n=== DELETE: A ===\n`=== DELETE: B ===`\n=== FILE: B ===\nb\n",
			files: []File{{Name: "A", Delete: true}, {Name: "B", Body: "b\n"}},
		},
		{
			name:  "EDIT blocks of one name add up, apart from a FILE of that name",
			reply: "=== EDIT: A ===\n--- REPLACE ---\nx\n\n=== FILE: A ===\na\n=== EDIT: B ===\nb\n=== EDIT: A ===\n--- AFTER ---\ny\n=== REPORT ===\nr\n",
			files: []File{
				{Name: "A", Body: "--- REPLACE ---\nx\n--- AFTER ---\ny\n", Edit: true},
				{Name: "A", Body: "a\n"},
				{Name: "B", Body: "b\n", Edit: true},
			},
			tail: "r\n",
		},
		{
			name:  "no markers: nothing",
			reply: "I could not do this.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, tail := Split(tt.reply)
			if !reflect.DeepEqual(files, tt.files) {
				t.Errorf("files = %+v, want %+v", files, tt.files)
			}
			if tail != tt.tail {
				t.Errorf("tail = %q, want %q", tail, tt.tail)
			}
		})
	}
}

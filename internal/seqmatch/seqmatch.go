// Package seqmatch compares texts. Ratio is the fuzzy half of the trace match
// (ADR 0001 §5): a Ratcliff/Obershelp similarity ratio. Changes is the line
// diff the format check reports a changed text with.
//
// Ratio's rule, ties included, is s2n's and doesn't change: the FUZZY
// threshold was calibrated on its numbers, and keeps its trialled meaning
// only while they stay the same.
package seqmatch

// Ratio is 2·M/T, where T is the number of code points in a and b together
// and M is the number of code points they have in matching blocks. The
// blocks are the longest common substring of a and b, then the same again on
// each side of it, recursively. Of several longest common substrings, the
// one that starts earliest in a is taken, then the one that starts earliest
// in b; the tie decides where the recursion splits, and so M. Ratio is 1 when
// both strings are empty.
//
// Strings are compared by code point, not by byte. They must be valid UTF-8:
// Go reads every invalid byte as U+FFFD, so two different invalid bytes
// compare equal.
func Ratio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra)+len(rb) == 0 {
		return 1.0
	}
	return 2.0 * float64(matched(ra, rb)) / float64(len(ra)+len(rb))
}

// matched is M: the code points in a's and b's matching blocks.
func matched(a, b []rune) int {
	i, j, k := longest(a, b)
	if k == 0 {
		return 0
	}
	return k + matched(a[:i], b[:j]) + matched(a[i+k:], b[j+k:])
}

// longest is the longest common substring of a and b, as a[i:i+k] and
// b[j:j+k]: of the longest, the one that starts earliest in a, then earliest
// in b.
func longest(a, b []rune) (besti, bestj, bestk int) {
	// prev[j+1] is the length of the common substring ending at a[i-1] and
	// b[j], cur[j+1] the one ending at a[i] and b[j].
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] != b[j] {
				cur[j+1] = 0
				continue
			}
			k := prev[j] + 1
			cur[j+1] = k
			// Of two equally long, the one that ends first starts first,
			// so the first found, by i and then j, is the one wanted.
			if k > bestk {
				besti, bestj, bestk = i-k+1, j-k+1, k
			}
		}
		prev, cur = cur, prev
	}
	return besti, bestj, bestk
}

// Changes is a line diff of a and b: `- <line>` for each line of a removed,
// and `+ <line>` for each line of b added, in order, with the lines kept left
// out. The lines kept are a longest common subsequence of a and b, so the
// diff is as short as a diff of removed and added lines can be.
//
// The lines removed and added between two kept lines, or before the first or
// after the last, are a hunk. Hunks are given in order, and within one, all
// its `-` lines come first, in a's order, then all its `+` lines, in b's.
//
// Where more than one longest common subsequence exists, the one kept is
// found by walking a and b from the start: a line is kept when the two
// lines are equal, and otherwise a's line is removed if a longest common
// subsequence remains without it, and b's added if not.
func Changes(a, b []string) []string {
	// The lines the two share at the start are kept, as the walk below would
	// keep them, and left out of the table. Those they share at the end
	// aren't: where a line repeats, the walk can keep an earlier copy of it.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	a, b = a[pre:], b[pre:]

	// lcs[i*w+j] is the length of the longest common subsequence of a[i:]
	// and b[j:].
	w := len(b) + 1
	lcs := make([]int, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}

	var out, removed, added []string
	flush := func() {
		out = append(append(out, removed...), added...)
		removed, added = removed[:0], added[:0]
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			flush()
			i, j = i+1, j+1
		case j == len(b) || i < len(a) && lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			removed = append(removed, "- "+a[i])
			i++
		default:
			added = append(added, "+ "+b[j])
			j++
		}
	}
	flush()
	return out
}

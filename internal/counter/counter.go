// Package counter counts strings, such as a segment's traces or quotes,
// keeping the order each was first counted. The split check and the trace
// diff count a segment's notes in name order and each note's lines in line
// order, so what they report lost or added is listed in the order the notes
// read, not in a map's or by sort.
package counter

// Counter counts strings. The zero value is empty and ready to use.
type Counter struct {
	keys   []string
	counts map[string]int
}

// Entry is one key of a Counter and its count.
type Entry struct {
	Key   string
	Count int
}

// Add counts k once more.
func (c *Counter) Add(k string) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	if _, ok := c.counts[k]; !ok {
		c.keys = append(c.keys, k)
	}
	c.counts[k]++
}

// Total is the sum of the counts.
func (c *Counter) Total() int {
	n := 0
	for _, k := range c.keys {
		n += c.counts[k]
	}
	return n
}

// Minus gives each key that c counts more of than o, with how many more, in
// the order c first counted them.
func (c *Counter) Minus(o *Counter) []Entry {
	out := []Entry{}
	for _, k := range c.keys {
		if n := c.counts[k] - o.counts[k]; n > 0 {
			out = append(out, Entry{Key: k, Count: n})
		}
	}
	return out
}

// Elements gives each entry's key as many times as its count, in the
// entries' order.
func Elements(entries []Entry) []string {
	out := []string{}
	for _, e := range entries {
		for range e.Count {
			out = append(out, e.Key)
		}
	}
	return out
}

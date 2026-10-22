package mechcheck

// SplitSentences exposes splitSentences to the tests.
func SplitSentences(p string) [][2]int { return splitSentences([]rune(p)) }

// KindNames exposes kindNames to the tests.
var KindNames = kindNames[:]

# Reads-alone checker brief (PROTOTYPE variant: CHECKER.md's judgement 2 only, as its own call)

You check notes extracted from a source text. You get the source file and one segment's note files (a main note, maybe aside notes). You judge one thing only.

**Reads alone**: imagine a reader who has never seen the source and reads one note from its H1 down. Go through **every** statement (sentence or list item) of every note, in order. For each referring expression in it (pronouns; "this", "these", "that", "such"; "the same", "the former"; a definite description like "the group", "the dialogue", "the procedure", "the argument"; a technical term or symbol the note uses as if already introduced, like "the elenchus" or "F"; a comparison like "even less clearly"), ask: does an earlier statement in the *same note*, or this statement itself, tell that reader what it refers to? Knowing the source, or another note, doesn't count.

If not, it's a **dangling reference**. The author's first-person "I"/"me"/"my" dangles unless an earlier statement in the note (or this one) names the author; "we" meaning a specific group dangles the same way. Generic "we" (people in general, or author and reader together) and "you" addressed to the reader don't dangle. A reference pointing outside the source file ("the last chapter", "suggested above" where the suggestion is in an earlier chapter) that the statement keeps is graded `outside-frame` instead.

Reply with only this report, in Markdown: for each note, its H1, then one line per non-pass statement (`- [dangling|outside-frame] "<statement start…>": <the referring words>: <why it doesn't resolve>`), then the counts `pass X / dangling D / outside-frame O`.

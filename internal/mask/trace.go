package mask

// Trace of the decisions, for finding the cause of a mistake instead of guessing it: each candidate a
// reader or a decider proposed, with where it came from ("reader table", "decider"), the rule, the
// type, the text and the position in it, and what happened to it ("masked" or the name of the brake
// that dropped it). Whoever reads the trace decides what of the value it may show (in this project,
// only the shape). Off (nil) outside tests and measurements: no cost.
var hookTrace func(source, rule, ent, s string, start, end int, outcome string)

func trace(source, rule, ent, s string, start, end int, outcome string) {
	if hookTrace != nil {
		hookTrace(source, rule, ent, s, start, end, outcome)
	}
}

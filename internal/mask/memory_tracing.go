package mask

import (
	"regexp"
	"sort"
	"strings"
)

// Tracing (see docs/structures.md): each client name is decided where it ENTERS the conversation
// and, from then on, is masked everywhere. Three rules:
//
//  1. Entry: only data (command output, file read) and the user text bring client names. The
//     text the assistant wrote is not a source: the model only knows the client through what
//     the proxy let through; a new word of its own is public knowledge (MaskContagion).
//  2. Contagion: the decided value applies to all occurrences, in any text and format.
//  3. Cleanup: public vocabulary is never marked (publicSystem, publicGeneral).
//
// Each mark carries its origin: decided with direct proof (reader, decider) or deduced (anchor by
// position). A deduction does not generate a deduction (no cascade), and only applies with seeds
// proven in the same source (the file or resource the command read): four table names that coincide
// with packages of a constraints.txt do not make the whole list become tables. A common word proven
// in one source does not contaminate another file read; in the conversation and in outputs without
// a file it is the name.

// addEntry: puts d into the memory (without repeating). Returns whether it entered.
func (mm *memory) addEntry(m *Masker, d Decision) bool {
	if d.Generic || len(d.Name) < memMin || publicGeneral(d.Name) || m.provenSoftware(d.Name) || !m.objMask(d.Ent) || isPseudoObj(d.Name) {
		return false
	}
	if mm.has == nil {
		mm.has = map[string]bool{}
	}
	k := d.Key()
	if mm.has[k] {
		return false
	}
	i := 0
	for i < len(d.Name) && !isWord(d.Name[i]) {
		i++
	}
	j := i
	for j < len(d.Name) && isWord(d.Name[j]) {
		j++
	}
	if i == j {
		return false
	}
	mm.has[k] = true
	nm := memName{name: d.Name, off: i, ent: d.Ent, caseless: entSQL[d.Ent]}
	idx, w := mm.exact, d.Name[i:j]
	if nm.caseless {
		idx, w = mm.caseless, strings.ToLower(w)
		mm.caselessSize |= bitSize(j - i)
	} else {
		mm.exactSize |= bitSize(j - i)
	}
	ns := append(idx[w], nm)
	sort.SliceStable(ns, func(a, b int) bool { return len(ns[a].name) > len(ns[b].name) })
	idx[w] = ns
	mm.n++
	return true
}

// learnAnchor: the new pieces of ts2 (outside before) are value decisions (rule 2): they enter the
// memory and are marked as deduced. Returns those that entered.
func (l *Batch) learnAnchor(s string, before, ts2 []snippet) []Decision {
	already := map[[2]int]bool{}
	for _, t := range before {
		already[[2]int{t.Start, t.End}] = true
	}
	var out []Decision
	for _, t := range ts2 {
		if already[[2]int{t.Start, t.End}] || !isObject(t.Type) || t.Type == objTypePrefix+genericEnt {
			continue
		}
		v := s[t.Start:t.End]
		if l.fromModel(v) {
			continue
		}
		d := Decision{Name: v, Ent: t.Type[len(objTypePrefix):], Rule: "âncora", Generic: isGeneric(v)}
		l.markDeduced(v)
		if l.mem.addEntry(l.m, d) {
			out = append(out, d)
		}
	}
	return out
}

func (l *Batch) markDeduced(v string) {
	if l.deducedSet == nil {
		l.deducedSet = map[string]bool{}
	}
	l.deducedSet[strings.ToLower(v)] = true
}

func (l *Batch) deduced(v string) bool { return l.deducedSet[strings.ToLower(v)] }

// MaskContagion (rule 1): a text the assistant wrote that does not match the record of what the
// proxy translated. It only receives the contagion: the format detectors (CPF, key, e-mail: they
// count by themselves), the values already marked and the conversation memory. No structure reader
// or decider runs here, and nothing is learned.
func (l *Batch) MaskContagion(s string, fromWeb bool, pos Position) (string, []Entry) {
	if len(s) < 4 {
		return s, nil
	}
	if r, ok := l.m.frozen(pos, s); ok {
		return r.text, r.entries
	}
	out := l.m.detectBase(s)
	longNum, _ := numericProfile(s)
	l.m.findKnown(s, longNum, func(ini, end int, typ string) {
		out = append(out, Finding{ini, end, typ, s[ini:end]})
	})
	l.m.unifyObjects(out)
	text, ents, ts := l.m.applyT(s, out)
	l.writing, l.currentSource = true, ""
	r := l.withMemory(s, result{text: text, entries: ents, snippets: ts, decidedSet: []Decision{}}, "")
	l.writing = false
	if _, already := l.outputs[pos]; !already {
		l.outputs[pos] = r
	}
	return r.text, r.entries
}

// publicSystem (rule 3): system vocabulary (SQL, development, devops, databases); never masked, not
// even in place.
func publicSystem(v string) bool {
	return publicSQL(v) || publicDev(v) || publicDevops(v) || databaseSchemes[strings.ToLower(v)]
}

// publicGeneral: the system one and the measured reference (publicRef). The reference, with proof,
// is masked in place, but does not spread.
func publicGeneral(v string) bool { return publicSystem(v) || publicRef[strings.ToLower(v)] }

// wholeValue (only the whole value is a candidate): s[a:b] is the whole value of its token, and not
// a piece of an expression. A quoted literal is a value; outside that, the token goes up to a space
// or a field separator (, ; | / : =), without the quotes and parentheses that wrap it. "pagamentos"
// in a column is; "CPF" in '.*CPF.*' or in (CPF|CNPJ) is not.
func wholeValue(s string, a, b int) bool {
	if a > 0 && b < len(s) && s[a-1] == s[b] && strings.IndexByte("'\"`", s[b]) >= 0 {
		return true
	}
	sep := func(c byte) bool { return strings.IndexByte(" \t\r\n,;|/:=", c) >= 0 }
	i, j := a, b
	for i > 0 && !sep(s[i-1]) {
		i--
	}
	for j < len(s) && !sep(s[j]) {
		j++
	}
	for i < a && strings.IndexByte("'\"`([{", s[i]) >= 0 {
		i++
	}
	for j > b && strings.IndexByte("'\"`)]}.", s[j-1]) >= 0 {
		j--
	}
	return i == a && j == b
}

// countOutsideQuotes: how many c there are in l outside single or double quotes (closed on the
// line).
func countOutsideQuotes(l string, c byte) int {
	n := 0
	var q byte
	for i := 0; i < len(l); i++ {
		switch {
		case q != 0:
			if l[i] == q {
				q = 0
			}
		case l[i] == '"' || l[i] == '\'':
			if strings.IndexByte(l[i+1:], l[i]) >= 0 {
				q = l[i]
			}
		case l[i] == c:
			n++
		}
	}
	return n
}

// noOverlapLong: the pieces in order, without overlap; in a conflict, the one that covers more
// stays (the whole result is never discarded because of a conflict).
func noOverlapLong(ts []snippet) []snippet {
	sortSnippets(ts)
	out := make([]snippet, 0, len(ts))
	for _, t := range ts {
		if n := len(out); n > 0 && t.Start < out[n-1].End {
			if t.End-t.Start > out[n-1].End-out[n-1].Start {
				out[n-1] = t
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

// lastCovering: the last piece (in order) that overlaps [a, b), or -1.
func lastCovering(ts []snippet, a, b int) int {
	i := sort.Search(len(ts), func(k int) bool { return ts[k].Start >= b })
	if k := i - 1; k >= 0 && ts[k].End > a {
		return k
	}
	return -1
}

// ---- fonte -------------------------------------------------------------------------------

// sourceSep: the source goes in front of the result hint (WithSource); the Batch splits it off on
// input.
const sourceSep = "\x1d"

// WithSource: the hint with the source (the files or the resource of the command) in front.
func WithSource(source, hint string) string {
	if source == "" {
		return hint
	}
	return sourceSep + source + sourceSep + hint
}

// SplitSource: the inverse of WithSource.
func SplitSource(d string) (source, hint string) {
	if !strings.HasPrefix(d, sourceSep) {
		return "", d
	}
	f, rest, _ := strings.Cut(d[len(sourceSep):], sourceSep)
	return f, rest
}

func noSources(items []BatchItem) []BatchItem {
	out := make([]BatchItem, len(items))
	for i, it := range items {
		_, it.Hint = SplitSource(it.Hint)
		out[i] = it
	}
	return out
}

// recordSources: the decisions with direct proof of each text are tied to its source.
func (l *Batch) recordSources(items []BatchItem, sources []string) {
	if l.sources == nil {
		l.sources = map[string]map[string]bool{}
	}
	for i, it := range items {
		if sources[i] == "" || it.FromWeb {
			continue
		}
		ds, _ := l.m.decidedMemo(it.S, it.Hint)
		for _, d := range ds {
			if d.Rule == "âncora" {
				continue
			}
			k := strings.ToLower(d.Name)
			if l.sources[k] == nil {
				l.sources[k] = map[string]bool{}
			}
			for _, f := range strings.Split(sources[i], ",") {
				l.sources[k][f] = true
			}
		}
	}
}

// provenAtSource: v was decided with proof in the text itself or in a source of the current text.
func (l *Batch) provenAtSource(v string) bool {
	k := strings.ToLower(v)
	if l.currentProv[k] {
		return true
	}
	if l.currentSource == "" {
		return false
	}
	for _, f := range strings.Split(l.currentSource, ",") {
		if l.sources[k][f] {
			return true
		}
	}
	return false
}

var reFileSource = regexp.MustCompile(`[\w~./\-]*[\w\-]\.[A-Za-z][\w]{0,5}\b`)

// CommandSource: the files the command cites (base name, lowercase); without a file, the program
// and the first argument that is not an option ("kubectl get").
func CommandSource(cmd string) string {
	vis := map[string]bool{}
	var fs []string
	for _, a := range reFileSource.FindAllString(cmd, 64) {
		b := strings.ToLower(a[strings.LastIndexAny(a, "/\\")+1:])
		if !vis[b] {
			vis[b] = true
			fs = append(fs, b)
		}
	}
	if len(fs) == 0 {
		var ws []string
		for _, w := range strings.Fields(cmd) {
			if !strings.HasPrefix(w, "-") {
				ws = append(ws, strings.ToLower(w))
			}
			if len(ws) == 2 {
				break
			}
		}
		if len(ws) > 0 {
			fs = append(fs, "cmd:"+strings.Join(ws, " "))
		}
	}
	sort.Strings(fs)
	return strings.Join(fs, ",")
}

// fileSource: the source cites a file (and not only a command without a file, or nothing).
func fileSource(f string) bool {
	for _, x := range strings.Split(f, ",") {
		if x != "" && !strings.HasPrefix(x, "cmd:") {
			return true
		}
	}
	return false
}

// documentExt: text documentation files (the content is prose, except the code blocks).
var documentExt = map[string]bool{"md": true, "markdown": true, "mdx": true, "txt": true, "rst": true, "adoc": true, "asciidoc": true}

// documentSource: every file of the source is documentation (with a code file along, the code rule
// applies, which protects more).
func documentSource(f string) bool {
	n := 0
	for _, x := range strings.Split(f, ",") {
		if x == "" || strings.HasPrefix(x, "cmd:") {
			continue
		}
		k := strings.LastIndexByte(x, '.')
		if k < 0 || !documentExt[x[k+1:]] {
			return false
		}
		n++
	}
	return n > 0
}

// SystemSource: the source of an output that describes or lists a system catalog (DESC/SHOW in
// SNOWFLAKE.ACCOUNT_USAGE, information_schema...). The listed structure is public documentation:
// the output is not a source of decisions (like the assistant text) and only receives the contagion
// of the client names already known.
const SystemSource = "sistema"

// SystemTarget: the command describes or lists a system catalog object.
func SystemTarget(cmd string) bool { return systemTarget(cmd) }

// UsePublic: the words (lowercase) the model wrote before data brought them in this conversation
// (priority, in the proxy). They are its knowledge: no path marks them here.
func (l *Batch) UsePublic(p map[string]bool) { l.publicSet = p }

// fromModel: v is model knowledge in this conversation (and does not contain a registered term: the
// registry always wins).
func (l *Batch) fromModel(v string) bool {
	if len(l.publicSet) == 0 {
		return false
	}
	return l.publicSet[strings.ToLower(v)] && !l.m.hasTerm(v)
}

// noPublics: the object pieces whose value is model knowledge come out (the format ones, like CPF
// and key, stay: they count by themselves).
func (l *Batch) noPublics(s string, ts []snippet) []snippet {
	out := ts[:0:0]
	for _, t := range ts {
		if v := s[t.Start:t.End]; isObject(t.Type) && l.fromModel(v) {
			continue
		}
		out = append(out, t)
	}
	return out
}

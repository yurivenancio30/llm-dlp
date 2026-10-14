package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// llm-dlp audit [PERIOD]: the summary of llm-dlp.log (and of the .1) over a period. It only reads
// the log, which only has counts and pseudonyms: nothing of the real data. It answers what used to
// require watching the session: what was masked, by type; whether some pseudonym came back to the
// client without becoming the real value (and may have ended up in a file); whether something went
// out without masking; what was refused.

var (
	reLogReq       = regexp.MustCompile(` -> (\d{3}) \| (\d+) (?:replacements|substituições) \| (\d+) (?:collisions|colisões)`)
	reLogTypes     = regexp.MustCompile(` \| (?:types|tipos): ([^|]+)`)
	reLogNotUndone = regexp.MustCompile(` \| (?:NOT UNDONE|NÃO DESFEITOS) \d+: ([^|]+)`)
	reLogCol       = regexp.MustCompile(` \| (?:collided|colididos): ([^|]+)`)
	reLogAPI       = regexp.MustCompile(` \| API: ([\w.-]+)`)
)

type logSummary struct {
	since                    time.Time
	req, substs, collisions  int
	collided                 []string
	status                   map[string]int // HTTP status other than 2xx
	apiErrors                map[string]int
	types                    map[string]int
	ndResponses              int
	pseudosNotUndone         []string
	unmasked, errs, refusedN int
	refusalReasons           map[string]int
}

func newSummary(since time.Time) *logSummary {
	return &logSummary{since: since, status: map[string]int{}, apiErrors: map[string]int{}, types: map[string]int{},
		refusalReasons: map[string]int{}}
}

// read adds the lines of the period to the summary (the lines start with "2006/01/02 15:04:05 ", in
// local time).
func (r *logSummary) readIt(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if len(l) < 20 {
			continue
		}
		t, err := time.ParseInLocation("2006/01/02 15:04:05", l[:19], time.Local)
		if err != nil || t.Before(r.since) {
			continue
		}
		msg := l[20:]
		switch {
		case strings.HasPrefix(msg, "REFUSED "), strings.HasPrefix(msg, "RECUSADO "): // (the Portuguese ones: a log up to 0.9)
			r.refusedN++
			if i := strings.Index(msg, ": "); i >= 0 {
				m := msg[i+2:]
				if len(m) > 70 {
					m = m[:70] + "…"
				}
				r.refusalReasons[m]++
			}
		case strings.HasPrefix(msg, "WARNING ") && strings.Contains(msg, "sent unmasked"),
			strings.HasPrefix(msg, "AVISO ") && strings.Contains(msg, "sem máscara"):
			r.unmasked++
		case strings.HasPrefix(msg, "ERROR "), strings.HasPrefix(msg, "ERRO "):
			r.errs++
		}
		m := reLogReq.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		r.req++
		if m[1][0] != '2' {
			r.status[m[1]]++
		}
		n, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		r.substs += n
		r.collisions += c
		if a := reLogAPI.FindStringSubmatch(msg); a != nil {
			r.apiErrors[strings.TrimSuffix(a[1], ":")]++
		}
		if tp := reLogTypes.FindStringSubmatch(msg); tp != nil {
			for _, pair := range strings.Split(tp[1], ",") {
				f := strings.Fields(pair)
				if len(f) == 2 {
					k, _ := strconv.Atoi(f[1])
					r.types[config.NewTypeName(f[0])] += k // a log up to 0.9 has the old names
				}
			}
		}
		if nd := reLogNotUndone.FindStringSubmatch(msg); nd != nil {
			r.ndResponses++
			r.pseudosNotUndone = joinList(r.pseudosNotUndone, nd[1])
		}
		if c := reLogCol.FindStringSubmatch(msg); c != nil {
			r.collided = joinList(r.collided, c[1])
		}
	}
}

// joinList adds the items of "a, b, c" that are not in xs yet (up to 40).
func joinList(xs []string, list string) []string {
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); p != "" && len(xs) < 40 && !containsStr(xs, p) {
			xs = append(xs, p)
		}
	}
	return xs
}

func containsStr(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// counts: "a 3, b 1", from largest to smallest.
func counts(m map[string]int) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return m[ks[i]] > m[ks[j]] || m[ks[i]] == m[ks[j]] && ks[i] < ks[j] })
	ps := make([]string, len(ks))
	for i, k := range ks {
		ps[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(ps, ", ")
}

func (r *logSummary) text(period string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "llm-dlp: log of the last %s (since %s)\n", period, r.since.Format("02/01 15:04"))
	fmt.Fprintf(&b, "  requests: %d", r.req)
	if len(r.status) > 0 {
		fmt.Fprintf(&b, " (status HTTP: %s)", counts(r.status))
	}
	b.WriteString("\n")
	if len(r.apiErrors) > 0 {
		fmt.Fprintf(&b, "  API errors: %s\n", counts(r.apiErrors))
	}
	fmt.Fprintf(&b, "  replacements: %d; collisions (pseudonym left masked): %d\n", r.substs, r.collisions)
	if len(r.collided) > 0 {
		fmt.Fprintf(&b, "    collided pseudonyms: %s\n", strings.Join(r.collided, ", "))
	}
	if len(r.types) > 0 {
		fmt.Fprintf(&b, "  masked by type (distinct values of each request, added up): %s\n", counts(r.types))
	}
	if r.ndResponses == 0 {
		b.WriteString("  pseudonyms that came back without turning into the real value: none\n")
	} else {
		fmt.Fprintf(&b, "  WARNING: %d response(s) reached the client with a pseudonym in place of the real value: %s\n",
			r.ndResponses, strings.Join(r.pseudosNotUndone, ", "))
		b.WriteString("    (look for them in the files written in this period: they kept the pseudonym)\n")
	}
	if r.unmasked > 0 {
		fmt.Fprintf(&b, "  WARNING: %d request(s) sent UNMASKED (fail_closed off and masking failed)\n", r.unmasked)
	} else {
		b.WriteString("  sent unmasked: none\n")
	}
	if r.refusedN > 0 {
		fmt.Fprintf(&b, "  refused: %d (%s)\n", r.refusedN, counts(r.refusalReasons))
	}
	if r.errs > 0 {
		fmt.Fprintf(&b, "  failures talking to the API: %d\n", r.errs)
	}
	return b.String()
}

// period: "24h", "90m", "7d" (days) or just the number of hours.
func period(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return time.Duration(n) * time.Hour, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid period: %q (use, for example, 24h, 90m or 7d)", s)
	}
	return d, nil
}

func audit(args []string) error {
	p := "24h"
	if len(args) > 0 {
		p = args[0]
	}
	d, err := period(p)
	if err != nil {
		return err
	}
	r := newSummary(time.Now().Add(-d))
	file := config.Path("llm-dlp.log")
	readOnes := 0
	for _, a := range []string{file + ".1", file} { // the old one first: time order
		f, err := os.Open(a)
		if err != nil {
			continue
		}
		r.readIt(f)
		f.Close()
		readOnes++
	}
	if readOnes == 0 {
		return fmt.Errorf("no log at %s", file)
	}
	fmt.Print(r.text(p))
	return nil
}

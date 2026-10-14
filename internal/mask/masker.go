// Package mask detects sensitive data and swaps it for pseudonyms derived from a secret key
// (HMAC-SHA256). The same real value always becomes the same pseudonym, and without the key there
// is no way to know which value a pseudonym stands for.
package mask

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/zricethezav/gitleaks/v8/detect"
)

// Masker: the object that masks. This file has the types and the construction; each stage is in its
// own file (see the code map in docs/development.md).

// Finding is a sensitive piece found in a text.
type Finding struct {
	Start, End int
	Type       string
	Real       string
}

// Entry links a pseudonym to the real value (lives only in memory, per request).
type Entry struct{ Pseudo, Real, Type string }

type result struct {
	text     string
	entries  []Entry
	snippets []snippet
	gen      int // how many "known values" existed when it was computed
	// noLearn: computed without learning (internet content)
	noLearn bool
	// decidedSet: the names decided in this text (see decision.go). RAM only.
	decidedSet []Decision
}

// Masker detects sensitive data and swaps it for pseudonyms. It is safe for concurrent use.
//
// The fields come in three groups: what is fixed after construction (configuration and rules), what
// the Masker remembers (each memory with its ceiling; the full table is in docs/development.md,
// "What is kept") and the caches.
type Masker struct {
	// ---- fixed after construction ----
	cfg        config.Config
	p          *Pseudo
	keyB64     string
	leaks      *detect.Detector // gitleaks: tokens and secrets with their own format
	readers    []Reader         // structure readers (readers.go)
	vocab      *vocabulary      // field names (fields_vocabulary.go)
	extras     []*regexp.Regexp // configuration patterns
	extraLabel []string
	terms      *regexp.Regexp // registered terms (always win)
	termLabel  map[string]string

	// ---- what the Masker remembers ----
	people     *People         // registered people, hash only, on disk (people.go)
	known      *knownSet       // values already masked, in RAM (memory_known.go)
	seenSet    *Seen           // the same values, hash only, on disk (memory_seen.go)
	sentSet    *Sent           // what each text already sent carried, on disk (memory_sent.go)
	writtenSet writtenRegistry // what the model wrote, to know who wrote it (decision.go)
	weakSet    weakSet         // weak name evidence: counts with two rules (objects.go)
	soft       sync.Map        // public software with structural proof (memory_software.go)
	localSoft  sync.Map        // names defined in the project: they cancel the proof (shadowing)

	// ---- caches (they only avoid redoing work) ----
	classes  sync.Map // field label -> class (fields_vocabulary.go)
	nClasses atomic.Int64

	mu    sync.Mutex
	memo  map[[32]byte]result // text -> result (mask.go); two generations
	stale map[[32]byte]result // previous generation of the memo
	bytes int
	// what already went out, by position in the conversation (see Batch); two generations, like the
	// memo. It is the RAM copy of the sent record
	frz, frzOld map[Position]result
	frzBytes    int
}

func NewMasker(cfg config.Config, key []byte, people *People, seenSet *Seen) (*Masker, error) {
	zerolog.SetGlobalLevel(zerolog.Disabled) // gitleaks logs to stderr; here we do not want noise
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}
	// decodes base64/hex/percent-encoding and looks for secrets inside (e.g. Kubernetes Secret,
	// which keeps everything in base64). The library default is 0 (does not decode).
	d.MaxDecodeDepth = 2
	m := &Masker{cfg: cfg, p: NewPseudo(key), leaks: d, people: people, seenSet: seenSet, known: newKnown(),
		memo: map[[32]byte]result{}, frz: map[Position]result{}, termLabel: map[string]string{},
		keyB64: base64.StdEncoding.EncodeToString(key), readers: readersOf(cfg)}
	// the traceback reader looks up the shadowing of this Masker (memory_software.go)
	m.readers = append(m.readers, Reader{Name: "traceback", Find: m.findTraceback, Public: publicDev})
	// field name vocabulary: the default plus what the user added
	m.vocab = defaultVocabulary()
	m.vocab.almost = cfg.Optional("quasi")
	for _, ce := range cfg.ExtraFields {
		for _, wd := range ce.Words {
			for _, t := range labelTokens(wd) {
				switch ce.Type {
				case "person":
					m.vocab.person[t] = true
				case "neutral":
					m.vocab.neutral[t] = true
				case "cpf", "cnpj", "rg", "cnh", "pis", "doc", "birthdate", "phone", "cep", "address", "user", "account", "card",
					"sensitive", "name", "quasi":
					m.vocab.class[t] = ce.Type
				default:
					return nil, fmt.Errorf("extra_fields: unknown type %q", ce.Type)
				}
			}
		}
	}
	for _, pe := range cfg.ExtraPatterns {
		re, err := regexp.Compile(pe.Regex)
		if err != nil {
			return nil, fmt.Errorf("extra pattern %q: %w", pe.Label, err)
		}
		m.extras = append(m.extras, re)
		m.extraLabel = append(m.extraLabel, pe.Label)
	}
	var alts []string
	for _, t := range cfg.Terms {
		for _, v := range t.Values {
			alts = append(alts, regexp.QuoteMeta(v))
			m.termLabel[strings.ToLower(v)] = t.Label
		}
	}
	if len(alts) > 0 {
		sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
		m.terms = regexp.MustCompile(`(?i)(?:` + strings.Join(alts, "|") + `)`)
	}
	return m, nil
}

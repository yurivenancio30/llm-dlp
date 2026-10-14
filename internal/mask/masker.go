// Package mask detecta dado sensível e o troca por pseudônimos derivados de uma
// chave secreta (HMAC-SHA256). O mesmo valor real vira sempre o mesmo pseudônimo,
// e sem a chave não há como saber a qual valor um pseudônimo corresponde.
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

// Masker: o objeto que mascara. Este arquivo tem os tipos e a construção; cada etapa está
// num arquivo próprio (ver o mapa do código em docs/pt-BR/development.md).

// Finding é um trecho sensível encontrado num texto.
type Finding struct {
	Start, End int
	Type       string
	Real       string
}

// Entry liga um pseudônimo ao valor real (vive só em memória, por requisição).
type Entry struct{ Pseudo, Real, Type string }

type result struct {
	text     string
	entries  []Entry
	snippets []snippet
	gen      int // quantos "valores conhecidos" existiam quando foi calculado
	// noLearn: calculado sem aprender (conteúdo da internet)
	noLearn bool
	// decididos: os nomes decididos neste texto (ver decision.go). Só em RAM.
	decidedSet []Decision
}

// Masker detecta e troca dado sensível por pseudônimos. É seguro para uso concorrente.
//
// Os campos vêm em três grupos: o que é fixo depois da construção (configuração e regras), o
// que o Masker lembra (cada memória com o seu teto; a tabela completa está em
// docs/pt-BR/development.md, "O que fica guardado") e os caches.
type Masker struct {
	// ---- fixo depois da construção ----
	cfg        config.Config
	p          *Pseudo
	keyB64     string
	leaks      *detect.Detector // gitleaks: tokens e segredos com formato próprio
	readers    []Reader         // leitores de estrutura (readers.go)
	vocab      *vocabulary      // nomes de campo (fields_vocabulary.go)
	extras     []*regexp.Regexp // padrões da configuração
	extraLabel []string
	terms      *regexp.Regexp // termos cadastrados (sempre vencem)
	termLabel  map[string]string

	// ---- o que o Masker lembra ----
	people     *People         // pessoas cadastradas, só hash, em disco (people.go)
	known      *knownSet       // valores já mascarados, em RAM (memory_known.go)
	seenSet    *Seen           // os mesmos valores, só hash, em disco (memory_seen.go)
	sentSet    *Sent           // o que cada texto já enviado levou, em disco (memory_sent.go)
	writtenSet writtenRegistry // o que o modelo escreveu, para saber quem escreveu (decision.go)
	weakSet    weakSet         // evidência fraca de nome: vale com duas regras (objects.go)
	soft       sync.Map        // software público com prova estrutural (memory_software.go)
	localSoft  sync.Map        // nomes definidos no projeto: anulam a prova (sombreamento)

	// ---- caches (só evitam refazer trabalho) ----
	classes  sync.Map // rótulo de campo -> classe (fields_vocabulary.go)
	nClasses atomic.Int64

	mu    sync.Mutex
	memo  map[[32]byte]result // texto -> resultado (mask.go); duas gerações
	stale map[[32]byte]result // geração anterior do memo
	bytes int
	// o que já saiu, por posição na conversa (ver Batch); duas gerações, como o memo. É a cópia
	// em RAM do registro de enviados
	frz, frzOld map[Position]result
	frzBytes    int
}

func NewMasker(cfg config.Config, key []byte, people *People, seenSet *Seen) (*Masker, error) {
	zerolog.SetGlobalLevel(zerolog.Disabled) // o gitleaks loga em stderr; aqui não queremos ruído
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}
	// decodifica base64/hex/percent-encoding e procura segredos lá dentro (ex.: Secret do
	// Kubernetes, que guarda tudo em base64). O padrão da biblioteca é 0 (não decodifica).
	d.MaxDecodeDepth = 2
	m := &Masker{cfg: cfg, p: NewPseudo(key), leaks: d, people: people, seenSet: seenSet, known: newKnown(),
		memo: map[[32]byte]result{}, frz: map[Position]result{}, termLabel: map[string]string{},
		keyB64: base64.StdEncoding.EncodeToString(key), readers: readersOf(cfg)}
	// o leitor de traceback consulta o sombreamento deste Masker (memory_software.go)
	m.readers = append(m.readers, Reader{Name: "traceback", Find: m.findTraceback, Public: publicDev})
	// vocabulário dos nomes de campo: o padrão mais o que o usuário acrescentou
	m.vocab = defaultVocabulary()
	m.vocab.almost = cfg.Optional("quase")
	for _, ce := range cfg.ExtraFields {
		for _, wd := range ce.Words {
			for _, t := range labelTokens(wd) {
				switch ce.Type {
				case "pessoa":
					m.vocab.person[t] = true
				case "neutra":
					m.vocab.neutral[t] = true
				case "cpf", "cnpj", "rg", "cnh", "pis", "doc", "nascimento", "telefone", "cep", "endereco", "usuario", "conta", "cartao",
					"sensivel", "nome", "quase":
					m.vocab.class[t] = ce.Type
				default:
					return nil, fmt.Errorf("campos_extras: tipo desconhecido %q", ce.Type)
				}
			}
		}
	}
	for _, pe := range cfg.ExtraPatterns {
		re, err := regexp.Compile(pe.Regex)
		if err != nil {
			return nil, fmt.Errorf("padrão extra %q: %w", pe.Label, err)
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

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
// num arquivo próprio (ver o mapa do código no README).

// Achado é um trecho sensível encontrado num texto.
type Achado struct {
	Ini, Fim int
	Tipo     string
	Real     string
}

// Entrada liga um pseudônimo ao valor real (vive só em memória, por requisição).
type Entrada struct{ Pseudo, Real, Tipo string }

type resultado struct {
	texto    string
	entradas []Entrada
	trechos  []trecho
	gen      int // quantos "valores conhecidos" existiam quando foi calculado
	// semAprender: calculado sem aprender (conteúdo da internet)
	semAprender bool
}

// Masker detecta e troca dado sensível por pseudônimos. É seguro para uso concorrente.
type Masker struct {
	cfg      config.Config
	p        *Pseudo
	leaks    *detect.Detector
	pessoas  *Pessoas
	vistos   *Vistos
	enviados *Enviados
	conh     *conhecidos
	leitores []Leitor // leitores de estrutura (objetos.go)
	fracos   fracos
	extras   []*regexp.Regexp
	rotExtra []string
	termos   *regexp.Regexp
	rotTermo map[string]string
	chaveB64 string

	vocab    *vocabulario
	classes  sync.Map // rótulo de campo -> classe (ver rotulos.go)
	nClasses atomic.Int64

	mu    sync.Mutex
	memo  map[[32]byte]resultado
	velho map[[32]byte]resultado // geração anterior do memo
	bytes int
	// o que já saiu, por posição na conversa (ver Lote); duas gerações, como o memo
	cong, congVelho map[Posicao]resultado
	congBytes       int
}

func NovoMasker(cfg config.Config, chave []byte, pessoas *Pessoas, vistos *Vistos) (*Masker, error) {
	zerolog.SetGlobalLevel(zerolog.Disabled) // o gitleaks loga em stderr; aqui não queremos ruído
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}
	// decodifica base64/hex/percent-encoding e procura segredos lá dentro (ex.: Secret do
	// Kubernetes, que guarda tudo em base64). O padrão da biblioteca é 0 (não decodifica).
	d.MaxDecodeDepth = 2
	m := &Masker{cfg: cfg, p: NovoPseudo(chave), leaks: d, pessoas: pessoas, vistos: vistos, conh: novosConhecidos(),
		memo: map[[32]byte]resultado{}, cong: map[Posicao]resultado{}, rotTermo: map[string]string{},
		chaveB64: base64.StdEncoding.EncodeToString(chave)}
	// vocabulário dos nomes de campo: o padrão mais o que o usuário acrescentou
	m.vocab = vocabularioPadrao()
	m.vocab.quase = cfg.Opcional("quase")
	for _, ce := range cfg.CamposExtras {
		for _, pal := range ce.Palavras {
			for _, t := range tokensRotulo(pal) {
				switch ce.Tipo {
				case "pessoa":
					m.vocab.pessoa[t] = true
				case "neutra":
					m.vocab.neutra[t] = true
				case "cpf", "cnpj", "rg", "cnh", "pis", "doc", "nascimento", "telefone", "cep", "endereco", "usuario", "conta", "cartao",
					"sensivel", "nome", "quase":
					m.vocab.classe[t] = ce.Tipo
				default:
					return nil, fmt.Errorf("campos_extras: tipo desconhecido %q", ce.Tipo)
				}
			}
		}
	}
	for _, pe := range cfg.PadroesExtras {
		re, err := regexp.Compile(pe.Regex)
		if err != nil {
			return nil, fmt.Errorf("padrão extra %q: %w", pe.Rotulo, err)
		}
		m.extras = append(m.extras, re)
		m.rotExtra = append(m.rotExtra, pe.Rotulo)
	}
	var alts []string
	for _, t := range cfg.Termos {
		for _, v := range t.Valores {
			alts = append(alts, regexp.QuoteMeta(v))
			m.rotTermo[strings.ToLower(v)] = t.Rotulo
		}
	}
	if len(alts) > 0 {
		sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
		m.termos = regexp.MustCompile(`(?i)(?:` + strings.Join(alts, "|") + `)`)
	}
	return m, nil
}

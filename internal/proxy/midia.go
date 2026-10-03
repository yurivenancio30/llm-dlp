package proxy

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"regexp"
	"strings"
	"sync"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/ocr"
)

// Imagens e PDFs: OCR, e cobre de preto o que for sensível.

// Midia trata imagens e PDFs que vão para a API: lê o texto (OCR/pdftotext),
// cobre o que é sensível e devolve blocos seguros. Resultados ficam memorizados
// (o Claude Code reenvia as mesmas imagens a cada mensagem).
type Midia struct {
	cfg   config.OCR
	o     *ocr.OCR
	m     *mask.Masker
	lib   []string // domínios liberados (não cobrir em imagens)
	dom   []string // domínios internos (cobrir mesmo com OCR imperfeito)
	mu    sync.Mutex
	memo  map[[32]byte]resMidia
	bytes int
}

type resMidia struct {
	blocos []any
	ents   []mask.Entrada
	limpa  bool // nada a cobrir: sai o bloco original (que não é guardado, para não segurar RAM)
	tam    int
}

// saida monta os blocos desta requisição. O marcador de cache (cache_control) muda de lugar
// a cada mensagem; por isso vem sempre do bloco atual e vai no último bloco que o substitui.
func (r resMidia) saida(b map[string]any) []any {
	if r.limpa {
		return []any{b}
	}
	cc, ok := b["cache_control"]
	if !ok {
		return r.blocos
	}
	out := append([]any(nil), r.blocos...)
	ult := copiar(out[len(out)-1].(map[string]any))
	ult["cache_control"] = cc
	out[len(out)-1] = ult
	return out
}

const memoMidiaMax = 128 << 20 // teto dos resultados memorizados; passou disso, recomeça

func NovaMidia(cfg config.Config, m *mask.Masker) *Midia {
	var env []string
	for k, v := range cfg.OCR.Env {
		env = append(env, k+"="+v)
	}
	return &Midia{cfg: cfg.OCR, m: m, lib: cfg.DominiosEmailLiberados, dom: cfg.DominiosInternos,
		memo: map[[32]byte]resMidia{},
		o: ocr.Novo(ocr.Config{Tesseract: cfg.OCR.Tesseract, PDFToText: cfg.OCR.PDFToText, PDFToPPM: cfg.OCR.PDFToPPM,
			Idioma: cfg.OCR.Idioma, Env: env})}
}

// Processar recebe um bloco image/document em base64 e devolve os blocos que podem sair.
func (md *Midia) Processar(b map[string]any) ([]any, []mask.Entrada, error) {
	src, _ := b["source"].(map[string]any)
	if src == nil || src["type"] != "base64" {
		return []any{b}, nil, nil // url/arquivo já hospedado: não há dado local aqui
	}
	tipo, _ := src["media_type"].(string)
	dados, _ := src["data"].(string)
	ehPDF := tipo == "application/pdf"
	if !ehPDF && !strings.HasPrefix(tipo, "image/") {
		return []any{b}, nil, nil
	}
	switch md.cfg.Modo {
	case "permitir":
		return []any{b}, nil, nil
	case "bloquear":
		return nil, nil, fmt.Errorf("envio de %s bloqueado pela configuração (ocr.modo = bloquear)", tipo)
	}
	k := sha256.Sum256([]byte(dados))
	md.mu.Lock()
	if r, ok := md.memo[k]; ok {
		md.mu.Unlock()
		return r.saida(b), r.ents, nil
	}
	md.mu.Unlock()

	bin, err := base64.StdEncoding.DecodeString(dados)
	if err != nil {
		return nil, nil, fmt.Errorf("%s com base64 inválido", tipo)
	}
	var r resMidia
	if ehPDF {
		r, err = md.pdf(b, bin)
	} else {
		r, err = md.imagem(b, bin)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("não consegui verificar %s: %w (para instalar o OCR: %s)", tipo, err, ocr.DicaInstalacao())
	}
	if r.limpa {
		r.blocos = nil
	}
	for _, bl := range r.blocos { // o cache_control é de cada requisição, não do resultado memorizado
		delete(bl.(map[string]any), "cache_control")
	}
	saida, ents := r.saida(b), r.ents
	md.mu.Lock()
	if md.bytes += r.tam + 64; md.bytes > memoMidiaMax {
		md.memo, md.bytes = map[[32]byte]resMidia{}, r.tam+64
	}
	md.memo[k] = r
	md.mu.Unlock()
	return saida, ents, nil
}

func (md *Midia) imagem(b map[string]any, bin []byte) (resMidia, error) {
	png, n, ents, err := md.cobrir(bin)
	if err != nil {
		return resMidia{}, err
	}
	if n == 0 {
		return resMidia{blocos: []any{b}, limpa: true}, nil
	}
	novo := copiar(b)
	dados := base64.StdEncoding.EncodeToString(png)
	novo["source"] = map[string]any{"type": "base64", "media_type": "image/png", "data": dados}
	return resMidia{blocos: []any{novo, nota(n, ents, "nesta imagem")}, ents: ents, tam: len(dados)}, nil
}

// cobrir: OCR + detecção; devolve a imagem (PNG) com os trechos cobertos.
func (md *Midia) cobrir(bin []byte) ([]byte, int, []mask.Entrada, error) {
	img, linhas, err := md.o.Ler(bin)
	if err != nil {
		return nil, 0, nil, err
	}
	var caixas []image.Rectangle
	var ents []mask.Entrada
	for _, l := range linhas {
		marcada := make([]bool, len(l.Palavras))
		for _, a := range md.m.Detectar(l.Texto) {
			for i, p := range l.Palavras {
				if l.Ini[i] < a.Fim && l.Ini[i]+len(p.Texto) > a.Ini {
					marcada[i] = true
				}
			}
			ents = append(ents, mask.Entrada{Pseudo: md.m.Pseudonimo(a.Tipo, a.Real), Real: a.Real, Tipo: a.Tipo})
		}
		for i, p := range l.Palavras {
			if marcada[i] || md.agressivo(p.Texto) {
				caixas = append(caixas, image.Rect(p.X, p.Y, p.X+p.W, p.Y+p.H))
			}
		}
	}
	if len(caixas) == 0 {
		return nil, 0, nil, nil
	}
	out, err := ocr.Cobrir(img, caixas)
	return out, len(caixas), dedup(ents), err
}

var (
	reTLD   = regexp.MustCompile(`(?i)[a-z0-9][\w.\-]*\.(com|net|org|gov|edu|io|br|intra|local|corp|interno|internal|lan|int)(\.[a-z]{2})?$`)
	reIPOCR = regexp.MustCompile(`\d{1,3}[.,]\d{1,3}[.,]\d{1,3}[.,]\d{1,3}`)
)

// agressivo: regras extras para imagem, porque o OCR erra (ex.: lê "@" como "Q").
// Imagem é rara; aqui vale cobrir a mais.
func (md *Midia) agressivo(w string) bool {
	w = strings.Trim(w, `()[]{}<>,;:'"`)
	if strings.Contains(w, "@") {
		return true
	}
	dig := 0
	for _, r := range w {
		if r >= '0' && r <= '9' {
			dig++
		}
	}
	if dig >= 8 || reIPOCR.MatchString(w) {
		return true
	}
	baixo := strings.ToLower(w)
	for _, d := range md.dom {
		if len(d) >= 6 && strings.Contains(baixo, strings.ToLower(d[:len(d)-1])) { // tolera 1 letra perdida no fim
			return true
		}
	}
	if len(w) >= 8 && reTLD.MatchString(w) {
		for _, l := range md.lib {
			if strings.HasSuffix(baixo, strings.ToLower(l)) {
				return false
			}
		}
		return true
	}
	return false
}

func (md *Midia) pdf(b map[string]any, bin []byte) (resMidia, error) {
	texto, err := md.o.TextoPDF(bin)
	if err != nil {
		return resMidia{}, err
	}
	if len(strings.TrimSpace(texto)) >= 40 {
		mas, ents := md.m.Mascarar(texto)
		if len(ents) == 0 && !md.cfg.PDFEstrito {
			return resMidia{blocos: []any{b}, limpa: true}, nil
		}
		novo := map[string]any{"type": "document",
			"source": map[string]any{"type": "text", "media_type": "text/plain", "data": mas}}
		titulo, _ := b["title"].(string)
		novo["title"] = strings.TrimSpace(titulo + " (texto extraído do PDF; dados sensíveis mascarados pelo llm-dlp)")
		for _, k := range []string{"context", "citations", "cache_control"} {
			if v, ok := b[k]; ok {
				novo[k] = v
			}
		}
		return resMidia{blocos: []any{novo}, ents: ents, tam: len(mas)}, nil
	}
	// PDF escaneado: cada página vira imagem e passa pelo OCR
	pags, err := md.o.PaginasPDF(bin, md.cfg.MaxPaginas)
	if err != nil {
		return resMidia{}, err
	}
	type res struct {
		png  []byte
		n    int
		ents []mask.Entrada
		err  error
	}
	out := make([]res, len(pags))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, p := range pags {
		wg.Add(1)
		go func(i int, p []byte) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			png, n, ents, err := md.cobrir(p)
			if n == 0 {
				png = p
			}
			out[i] = res{png, n, ents, err}
		}(i, p)
	}
	wg.Wait()
	total := 0
	var ents []mask.Entrada
	for _, r := range out {
		if r.err != nil {
			return resMidia{}, r.err
		}
		total += r.n
		ents = append(ents, r.ents...)
	}
	if total == 0 && !md.cfg.PDFEstrito {
		return resMidia{blocos: []any{b}, limpa: true}, nil
	}
	var blocos []any
	tam := 0
	for _, r := range out {
		dados := base64.StdEncoding.EncodeToString(r.png)
		tam += len(dados)
		blocos = append(blocos, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": dados}})
	}
	blocos = append(blocos, nota(total, dedup(ents), fmt.Sprintf("nas %d páginas acima (PDF escaneado)", len(pags))))
	return resMidia{blocos: blocos, ents: dedup(ents), tam: tam}, nil
}

func nota(n int, ents []mask.Entrada, onde string) map[string]any {
	t := fmt.Sprintf("[llm-dlp] %d trecho(s) com dado sensível foram cobertos %s.", n, onde)
	if len(ents) > 0 {
		var ps []string
		for _, e := range ents {
			ps = append(ps, e.Pseudo)
		}
		t += " Entre eles: " + strings.Join(ps, ", ") + "."
	}
	return map[string]any{"type": "text", "text": t}
}

func dedup(ents []mask.Entrada) []mask.Entrada {
	visto := map[string]bool{}
	var out []mask.Entrada
	for _, e := range ents {
		if !visto[e.Pseudo] {
			visto[e.Pseudo] = true
			out = append(out, e)
		}
	}
	return out
}

func copiar(b map[string]any) map[string]any {
	n := make(map[string]any, len(b))
	for k, v := range b {
		n[k] = v
	}
	return n
}

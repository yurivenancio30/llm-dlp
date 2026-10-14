package proxy

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
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

// Media trata imagens e PDFs que vão para a API: lê o texto (OCR/pdftotext),
// cobre o que é sensível e devolve blocos seguros. Resultados ficam memorizados
// (o Claude Code reenvia as mesmas imagens a cada mensagem). O arquivo que não deu para
// verificar nunca sai: é retirado, e no lugar vai um aviso (ver retirar).
type Media struct {
	cfg   config.OCR
	o     *ocr.OCR
	m     *mask.Masker
	lib   []string // domínios liberados (não cobrir em imagens)
	dom   []string // domínios internos (cobrir mesmo com OCR imperfeito)
	mu    sync.Mutex
	memo  map[[32]byte]mediaRes
	bytes int
	// Warning recebe o motivo de cada arquivo retirado (o proxy grava no log)
	Warning func(msg string)
	warned  map[[32]byte]bool
}

type mediaRes struct {
	blocks []any
	ents   []mask.Entry
	clean  bool // nada a cobrir: sai o bloco original (que não é guardado, para não segurar RAM)
	size   int
}

// saida monta os blocos desta requisição. O marcador de cache (cache_control) muda de lugar
// a cada mensagem; por isso vem sempre do bloco atual e vai no último bloco que o substitui.
func (r mediaRes) output(b map[string]any) []any {
	if r.clean {
		return []any{b}
	}
	cc, ok := b["cache_control"]
	if !ok {
		return r.blocks
	}
	out := append([]any(nil), r.blocks...)
	lastPos := copyPath(out[len(out)-1].(map[string]any))
	lastPos["cache_control"] = cc
	out[len(out)-1] = lastPos
	return out
}

const mediaMemoMax = 128 << 20 // teto dos resultados memorizados; passou disso, recomeça

func NewMedia(cfg config.Config, m *mask.Masker) *Media {
	var env []string
	for k, v := range cfg.OCR.Env {
		env = append(env, k+"="+v)
	}
	return &Media{cfg: cfg.OCR, m: m, lib: cfg.AllowedEmailDomains, dom: cfg.InternalDomains,
		memo: map[[32]byte]mediaRes{}, warned: map[[32]byte]bool{},
		o: ocr.New(ocr.Config{Tesseract: cfg.OCR.Tesseract, PDFToText: cfg.OCR.PDFToText, PDFToPPM: cfg.OCR.PDFToPPM,
			Language: cfg.OCR.Language, Env: env})}
}

// Process recebe um bloco image/document em base64 e devolve os blocos que podem sair.
func (md *Media) Process(b map[string]any) ([]any, []mask.Entry, error) {
	src, _ := b["source"].(map[string]any)
	if src == nil || src["type"] != "base64" {
		return []any{b}, nil, nil // url/arquivo já hospedado: não há dado local aqui
	}
	typ, _ := src["media_type"].(string)
	data, _ := src["data"].(string)
	isPDF := typ == "application/pdf"
	if !isPDF && !strings.HasPrefix(typ, "image/") {
		return []any{b}, nil, nil
	}
	switch md.cfg.Mode {
	case "allow":
		return []any{b}, nil, nil
	case "block":
		return md.removeIt(b, isPDF, "o envio de imagens e PDFs está bloqueado na configuração do llm-dlp (ocr.mode = block). Avise o usuário"), nil, nil
	}
	k := sha256.Sum256([]byte(data))
	md.mu.Lock()
	if r, ok := md.memo[k]; ok {
		md.mu.Unlock()
		return r.output(b), r.ents, nil
	}
	md.mu.Unlock()

	bin, err := base64.StdEncoding.DecodeString(data)
	var r mediaRes
	if err != nil {
		err = errors.New("base64 inválido")
	} else if isPDF {
		r, err = md.pdf(b, bin)
	} else {
		r, err = md.image(b, bin)
	}
	if err != nil {
		md.warn(k, fmt.Sprintf("%q retirado da requisição, não deu para verificar: %v", typ, err))
		reason := "o llm-dlp não conseguiu verificar se o arquivo tem dado sensível, porque "
		if ocr.Missing(err) {
			reason += "o OCR não está instalado nesta máquina. Avise o usuário: para enviar imagens e PDFs, instalar o OCR (" +
				ocr.InstallHint() + ") e mandar o arquivo de novo"
		} else {
			reason += "a leitura do arquivo falhou. Avise o usuário: o motivo está no log do llm-dlp (llm-dlp.log); se o OCR estiver incompleto: " +
				ocr.InstallHint()
		}
		return md.removeIt(b, isPDF, reason), nil, nil
	}
	if r.clean {
		r.blocks = nil
	}
	for _, bl := range r.blocks { // o cache_control é de cada requisição, não do resultado memorizado
		delete(bl.(map[string]any), "cache_control")
	}
	output, ents := r.output(b), r.ents
	md.mu.Lock()
	if md.bytes += r.size + 64; md.bytes > mediaMemoMax {
		md.memo, md.bytes = map[[32]byte]mediaRes{}, r.size+64
	}
	md.memo[k] = r
	md.mu.Unlock()
	return output, ents, nil
}

// retirar: a imagem ou o PDF que não deu para verificar (ou que a configuração bloqueia) não
// sai; no lugar vai um aviso, para o modelo contar ao usuário o que houve e como resolver.
// Recusar a requisição inteira prendia a conversa: o arquivo fica no histórico, que o Claude
// Code reenvia a cada mensagem. O aviso só tem texto fixo (o detalhe do erro, que pode citar um
// caminho da máquina, fica no log) e o resultado não é memorizado: com o OCR instalado, a
// próxima mensagem já leva o arquivo.
func (md *Media) removeIt(b map[string]any, isPDF bool, reason string) []any {
	t := "[llm-dlp] Uma imagem foi retirada daqui e não foi enviada: "
	if isPDF {
		t = "[llm-dlp] Um PDF foi retirado daqui e não foi enviado: "
	}
	return mediaRes{blocks: []any{map[string]any{"type": "text", "text": t + reason + "."}}}.output(b)
}

// avisar grava no log, uma vez por arquivo, por que ele foi retirado.
func (md *Media) warn(k [32]byte, msg string) {
	md.mu.Lock()
	already := md.warned[k]
	if len(md.warned) > 4096 {
		md.warned = map[[32]byte]bool{}
	}
	md.warned[k] = true
	md.mu.Unlock()
	if !already && md.Warning != nil {
		md.Warning(msg)
	}
}

func (md *Media) image(b map[string]any, bin []byte) (mediaRes, error) {
	png, n, ents, err := md.cover(bin)
	if err != nil {
		return mediaRes{}, err
	}
	if n == 0 {
		return mediaRes{blocks: []any{b}, clean: true}, nil
	}
	newOne := copyPath(b)
	data := base64.StdEncoding.EncodeToString(png)
	newOne["source"] = map[string]any{"type": "base64", "media_type": "image/png", "data": data}
	return mediaRes{blocks: []any{newOne, note(n, ents, "nesta imagem")}, ents: ents, size: len(data)}, nil
}

// cobrir: OCR + detecção; devolve a imagem (PNG) com os trechos cobertos.
func (md *Media) cover(bin []byte) ([]byte, int, []mask.Entry, error) {
	img, lines, err := md.o.Read(bin)
	if err != nil {
		return nil, 0, nil, err
	}
	var caseForms []image.Rectangle
	var ents []mask.Entry
	for _, l := range lines {
		marked := make([]bool, len(l.Words))
		for _, a := range md.m.Detect(l.Text) {
			for i, p := range l.Words {
				if l.Start[i] < a.End && l.Start[i]+len(p.Text) > a.Start {
					marked[i] = true
				}
			}
			ents = append(ents, mask.Entry{Pseudo: md.m.Pseudonym(a.Type, a.Real), Real: a.Real, Type: a.Type})
		}
		for i, p := range l.Words {
			if marked[i] || md.aggressive(p.Text) {
				caseForms = append(caseForms, image.Rect(p.X, p.Y, p.X+p.W, p.Y+p.H))
			}
		}
	}
	if len(caseForms) == 0 {
		return nil, 0, nil, nil
	}
	out, err := ocr.Cover(img, caseForms)
	return out, len(caseForms), dedup(ents), err
}

var (
	reTLD   = regexp.MustCompile(`(?i)[a-z0-9][\w.\-]*\.(com|net|org|gov|edu|io|br|intra|local|corp|interno|internal|lan|int)(\.[a-z]{2})?$`)
	reIPOCR = regexp.MustCompile(`\d{1,3}[.,]\d{1,3}[.,]\d{1,3}[.,]\d{1,3}`)
)

// agressivo: regras extras para imagem, porque o OCR erra (ex.: lê "@" como "Q").
// Imagem é rara; aqui vale cobrir a mais.
func (md *Media) aggressive(w string) bool {
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
	lower := strings.ToLower(w)
	for _, d := range md.dom {
		if len(d) >= 6 && strings.Contains(lower, strings.ToLower(d[:len(d)-1])) { // tolera 1 letra perdida no fim
			return true
		}
	}
	if len(w) >= 8 && reTLD.MatchString(w) {
		for _, l := range md.lib {
			if strings.HasSuffix(lower, strings.ToLower(l)) {
				return false
			}
		}
		return true
	}
	return false
}

func (md *Media) pdf(b map[string]any, bin []byte) (mediaRes, error) {
	text, err := md.o.PDFText(bin)
	if err != nil {
		return mediaRes{}, err
	}
	if len(strings.TrimSpace(text)) >= 40 {
		masked, ents := md.m.Mask(text)
		if len(ents) == 0 && !md.cfg.StrictPDF {
			return mediaRes{blocks: []any{b}, clean: true}, nil
		}
		newOne := map[string]any{"type": "document",
			"source": map[string]any{"type": "text", "media_type": "text/plain", "data": masked}}
		title, _ := b["title"].(string)
		newOne["title"] = strings.TrimSpace(title + " (texto extraído do PDF; dados sensíveis mascarados pelo llm-dlp)")
		for _, k := range []string{"context", "citations", "cache_control"} {
			if v, ok := b[k]; ok {
				newOne[k] = v
			}
		}
		return mediaRes{blocks: []any{newOne}, ents: ents, size: len(masked)}, nil
	}
	// PDF escaneado: cada página vira imagem e passa pelo OCR
	pages, err := md.o.PDFPages(bin, md.cfg.MaxPages)
	if err != nil {
		return mediaRes{}, err
	}
	type res struct {
		png  []byte
		n    int
		ents []mask.Entry
		err  error
	}
	out := make([]res, len(pages))
	var wg sync.WaitGroup
	without := make(chan struct{}, 4)
	for i, p := range pages {
		wg.Add(1)
		go func(i int, p []byte) {
			defer wg.Done()
			without <- struct{}{}
			defer func() { <-without }()
			png, n, ents, err := md.cover(p)
			if n == 0 {
				png = p
			}
			out[i] = res{png, n, ents, err}
		}(i, p)
	}
	wg.Wait()
	total := 0
	var ents []mask.Entry
	for _, r := range out {
		if r.err != nil {
			return mediaRes{}, r.err
		}
		total += r.n
		ents = append(ents, r.ents...)
	}
	if total == 0 && !md.cfg.StrictPDF {
		return mediaRes{blocks: []any{b}, clean: true}, nil
	}
	var blocks []any
	size := 0
	for _, r := range out {
		data := base64.StdEncoding.EncodeToString(r.png)
		size += len(data)
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": data}})
	}
	blocks = append(blocks, note(total, dedup(ents), fmt.Sprintf("nas %d páginas acima (PDF escaneado)", len(pages))))
	return mediaRes{blocks: blocks, ents: dedup(ents), size: size}, nil
}

func note(n int, ents []mask.Entry, where string) map[string]any {
	t := fmt.Sprintf("[llm-dlp] %d trecho(s) com dado sensível foram cobertos %s.", n, where)
	if len(ents) > 0 {
		var ps []string
		for _, e := range ents {
			ps = append(ps, e.Pseudo)
		}
		t += " Entre eles: " + strings.Join(ps, ", ") + "."
	}
	return map[string]any{"type": "text", "text": t}
}

func dedup(ents []mask.Entry) []mask.Entry {
	seen := map[string]bool{}
	var out []mask.Entry
	for _, e := range ents {
		if !seen[e.Pseudo] {
			seen[e.Pseudo] = true
			out = append(out, e)
		}
	}
	return out
}

func copyPath(b map[string]any) map[string]any {
	n := make(map[string]any, len(b))
	for k, v := range b {
		n[k] = v
	}
	return n
}

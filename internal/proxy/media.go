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

// Images and PDFs: OCR, and blacks out what is sensitive.

// Media handles images and PDFs that go to the API: reads the text (OCR/pdftotext), covers what is
// sensitive and returns safe blocks. Results are memoized (Claude Code resends the same images with
// every message). A file that could not be checked never goes out: it is removed, and a notice goes
// in its place (see removeIt).
type Media struct {
	cfg   config.OCR
	o     *ocr.OCR
	m     *mask.Masker
	lib   []string // allowed domains (do not cover in images)
	dom   []string // internal domains (cover even with imperfect OCR)
	mu    sync.Mutex
	memo  map[[32]byte]mediaRes
	bytes int
	// Warning receives the reason for each removed file (the proxy writes it to the log)
	Warning func(msg string)
	warned  map[[32]byte]bool
}

type mediaRes struct {
	blocks []any
	ents   []mask.Entry
	clean  bool // nothing to cover: the original block goes out (which is not kept, to not hold RAM)
	size   int
}

// output builds the blocks of this request. The cache marker (cache_control) moves with every
// message; so it always comes from the current block and goes on the last block that replaces it.
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

const mediaMemoMax = 128 << 20 // ceiling of memoized results; past that, start over

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

// Process receives an image/document block in base64 and returns the blocks that may go out.
func (md *Media) Process(b map[string]any) ([]any, []mask.Entry, error) {
	src, _ := b["source"].(map[string]any)
	if src == nil || src["type"] != "base64" {
		return []any{b}, nil, nil // url/file already hosted: there is no local data here
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
		return md.removeIt(b, isPDF, "sending images and PDFs is blocked in the llm-dlp configuration (ocr.mode = block). Tell the user"), nil, nil
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
		err = errors.New("invalid base64")
	} else if isPDF {
		r, err = md.pdf(b, bin)
	} else {
		r, err = md.image(b, bin)
	}
	if err != nil {
		md.warn(k, fmt.Sprintf("%q removed from the request, it could not be checked: %v", typ, err))
		reason := "llm-dlp could not check whether the file has sensitive data, because "
		if ocr.Missing(err) {
			reason += "OCR is not installed on this machine. Tell the user: to send images and PDFs, install OCR (" +
				ocr.InstallHint() + ") and send the file again"
		} else {
			reason += "reading the file failed. Tell the user: the reason is in the llm-dlp log (llm-dlp.log); if OCR is incomplete: " +
				ocr.InstallHint()
		}
		return md.removeIt(b, isPDF, reason), nil, nil
	}
	if r.clean {
		r.blocks = nil
	}
	for _, bl := range r.blocks { // cache_control belongs to each request, not to the memoized result
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

// removeIt: the image or PDF that could not be checked (or that the configuration blocks) does not
// go out; a notice goes in its place, for the model to tell the user what happened and how to fix
// it. Refusing the whole request got the conversation stuck: the file stays in the history, which
// Claude Code resends with every message. The notice only has fixed text (the error detail, which
// may cite a path on the machine, stays in the log) and the result is not memoized: with OCR
// installed, the next message already carries the file.
func (md *Media) removeIt(b map[string]any, isPDF bool, reason string) []any {
	t := "[llm-dlp] An image was removed from here and was not sent: "
	if isPDF {
		t = "[llm-dlp] A PDF was removed from here and was not sent: "
	}
	return mediaRes{blocks: []any{map[string]any{"type": "text", "text": t + reason + "."}}}.output(b)
}

// warn writes to the log, once per file, why it was removed.
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
	return mediaRes{blocks: []any{newOne, note(n, ents, "in this image")}, ents: ents, size: len(data)}, nil
}

// cover: OCR + detection; returns the image (PNG) with the pieces covered.
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

// aggressive: extra rules for images, because OCR makes mistakes (e.g. reads "@" as "Q"). Images
// are rare; here it is worth covering more.
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
		if len(d) >= 6 && strings.Contains(lower, strings.ToLower(d[:len(d)-1])) { // tolerates 1 letter lost at the end
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
		newOne["title"] = strings.TrimSpace(title + " (text extracted from the PDF; sensitive data masked by llm-dlp)")
		for _, k := range []string{"context", "citations", "cache_control"} {
			if v, ok := b[k]; ok {
				newOne[k] = v
			}
		}
		return mediaRes{blocks: []any{newOne}, ents: ents, size: len(masked)}, nil
	}
	// scanned PDF: each page becomes an image and goes through OCR
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
	blocks = append(blocks, note(total, dedup(ents), fmt.Sprintf("in the %d pages above (scanned PDF)", len(pages))))
	return mediaRes{blocks: blocks, ents: dedup(ents), size: size}, nil
}

func note(n int, ents []mask.Entry, where string) map[string]any {
	t := fmt.Sprintf("[llm-dlp] %d piece(s) with sensitive data were covered %s.", n, where)
	if len(ents) > 0 {
		var ps []string
		for _, e := range ents {
			ps = append(ps, e.Pseudo)
		}
		t += " Among them: " + strings.Join(ps, ", ") + "."
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

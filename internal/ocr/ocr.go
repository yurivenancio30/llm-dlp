// Package ocr lê o texto de imagens e PDFs (tesseract/poppler) e cobre, na imagem,
// os trechos que o mascarador considera sensíveis.
package ocr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

type Config struct {
	Tesseract string
	PDFToText string
	PDFToPPM  string
	Language  string
	Env       []string // ex.: LD_LIBRARY_PATH, TESSDATA_PREFIX quando instalado fora do padrão
	Timeout   time.Duration
}

type OCR struct{ c Config }

func New(c Config) *OCR {
	if c.Language == "" {
		c.Language = "por"
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return &OCR{c: c}
}

// Available confere se o tesseract roda (para falhar fechado com mensagem clara se não).
func (o *OCR) Available() error {
	_, err := o.runIt(context.Background(), nil, o.c.Tesseract, "--version")
	if err != nil {
		return fmt.Errorf("tesseract indisponível (%s): %w", o.c.Tesseract, err)
	}
	return nil
}

type Word struct {
	Text                string
	X, Y, W, H          int
	Block, Pair, LineNo int
}

// Line é uma linha de texto montada a partir das palavras, com a posição de cada uma.
type Line struct {
	Text  string
	Words []Word
	Start []int // início de cada palavra dentro de Text
}

// Read devolve a imagem decodificada e as linhas de texto encontradas nela.
func (o *OCR) Read(data []byte) (image.Image, []Line, error) {
	// o tamanho declarado no cabeçalho, antes de decodificar: um arquivo de poucos KB pode
	// declarar uma imagem de gigabytes e derrubar o processo por falta de memória
	if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, nil, fmt.Errorf("imagem ilegível: %w", err)
	} else if c.Width <= 0 || c.Height <= 0 || c.Width > sideMax || c.Height > sideMax || c.Width*c.Height > maxDots {
		return nil, nil, fmt.Errorf("imagem grande demais (%d x %d pontos)", c.Width, c.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("imagem ilegível: %w", err)
	}
	// tela escura (tema escuro, terminal): inverte antes do OCR, que lê melhor preto no branco
	entry := img
	if dark(img) {
		entry = reverse(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, entry); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout)
	defer cancel()
	out, err := o.runIt(ctx, buf.Bytes(), o.c.Tesseract, "stdin", "stdout", "-l", o.c.Language, "-c", "tessedit_do_invert=0", "tsv")
	if err != nil {
		return nil, nil, fmt.Errorf("tesseract: %w", err)
	}
	return img, lines(parseTSV(out)), nil
}

func parseTSV(b []byte) []Word {
	var ps []Word
	for i, l := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue
		}
		c := strings.Split(l, "\t")
		if len(c) < 12 || c[0] != "5" || strings.TrimSpace(c[11]) == "" {
			continue
		}
		n := func(k int) int { v, _ := strconv.Atoi(c[k]); return v }
		ps = append(ps, Word{Text: c[11], Block: n(2), Pair: n(3), LineNo: n(4), X: n(6), Y: n(7), W: n(8), H: n(9)})
	}
	return ps
}

func lines(ps []Word) []Line {
	groups := map[[3]int][]Word{}
	var order [][3]int
	for _, p := range ps {
		k := [3]int{p.Block, p.Pair, p.LineNo}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}
	var out []Line
	for _, k := range order {
		ws := groups[k]
		sort.Slice(ws, func(i, j int) bool { return ws[i].X < ws[j].X })
		var l Line
		for i, w := range ws {
			if i > 0 {
				l.Text += " "
			}
			l.Start = append(l.Start, len(l.Text))
			l.Text += w.Text
			l.Words = append(l.Words, w)
		}
		out = append(out, l)
	}
	return out
}

// Cover pinta retângulos pretos (com folga) sobre as caixas e devolve PNG.
func Cover(img image.Image, caseForms []image.Rectangle) ([]byte, error) {
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	for _, r := range caseForms {
		r = image.Rect(r.Min.X-3, r.Min.Y-3, r.Max.X+3, r.Max.Y+3).Intersect(b)
		draw.Draw(rgba, r, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, rgba)
	return buf.Bytes(), err
}

// PDFText extrai o texto de um PDF (vazio para PDF escaneado).
func (o *OCR) PDFText(pdf []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout)
	defer cancel()
	out, err := o.runIt(ctx, pdf, o.c.PDFToText, "-layout", "-", "-")
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(out), nil
}

// PDFPages renderiza as páginas do PDF como PNG (para OCR de PDF escaneado).
func (o *OCR) PDFPages(pdf []byte, maxPages int) ([][]byte, error) {
	dir, err := os.MkdirTemp("", "llm-dlp-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(file, pdf, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout*time.Duration(maxPages/10+1))
	defer cancel()
	if _, err := o.runIt(ctx, nil, o.c.PDFToPPM, "-r", "150", "-png", "-l", strconv.Itoa(maxPages+1), file, filepath.Join(dir, "p")); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w", err)
	}
	names, _ := filepath.Glob(filepath.Join(dir, "p-*.png"))
	sort.Strings(names)
	if len(names) > maxPages {
		return nil, fmt.Errorf("PDF com mais de %d páginas", maxPages)
	}
	var pages [][]byte
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		pages = append(pages, b)
	}
	return pages, nil
}

func (o *OCR) runIt(ctx context.Context, entry []byte, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), o.c.Env...)
	if entry != nil {
		cmd.Stdin = bytes.NewReader(entry)
	}
	out, errb := &limited{max: maxOutput}, &limited{max: 64 << 10, discards: true}
	cmd.Stdout, cmd.Stderr = out, errb
	if err := cmd.Run(); err != nil {
		if out.overflowed {
			return nil, fmt.Errorf("a saída de %s passou de %d MB", filepath.Base(bin), maxOutput>>20)
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.b.String()))
	}
	return out.b.Bytes(), nil
}

// Limites contra arquivo montado para estourar a memória. A API não aceita imagem com lado maior
// que 8000 pontos; o texto de um PDF de verdade não chega perto de maxOutput.
const (
	sideMax   = 16000
	maxDots   = 64 << 20
	maxOutput = 64 << 20
)

// limitado guarda até max bytes. Passou disso: com descarta, o resto é jogado fora (as mensagens
// de erro do programa); sem, a escrita falha e o programa é encerrado (a saída).
type limited struct {
	b          bytes.Buffer
	max        int
	discards   bool
	overflowed bool
}

func (l *limited) Write(p []byte) (int, error) {
	if l.b.Len()+len(p) > l.max {
		if l.discards {
			return len(p), nil
		}
		l.overflowed = true
		return 0, errors.New("saída grande demais")
	}
	return l.b.Write(p)
}

// Missing: o erro é de programa não instalado (tesseract ou poppler fora do PATH).
func Missing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}

// escura: luminância média baixa (amostrada, para ser barato).
func dark(img image.Image) bool {
	b := img.Bounds()
	var sum, n uint64
	step := max(1, b.Dx()/200)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += (299*uint64(r) + 587*uint64(g) + 114*uint64(bl)) / 1000
			n++
		}
	}
	return n > 0 && sum/n < 0x8000
}

func reverse(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			out.Set(x, y, color.RGBA64{uint16(0xffff - r), uint16(0xffff - g), uint16(0xffff - bl), uint16(a)})
		}
	}
	return out
}

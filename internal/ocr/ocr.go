// Package ocr lê o texto de imagens e PDFs (tesseract/poppler) e cobre, na imagem,
// os trechos que o mascarador considera sensíveis.
package ocr

import (
	"bytes"
	"context"
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
	Idioma    string
	Env       []string // ex.: LD_LIBRARY_PATH, TESSDATA_PREFIX quando instalado fora do padrão
	Timeout   time.Duration
}

type OCR struct{ c Config }

func Novo(c Config) *OCR {
	if c.Idioma == "" {
		c.Idioma = "por"
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return &OCR{c: c}
}

// Disponivel confere se o tesseract roda (para falhar fechado com mensagem clara se não).
func (o *OCR) Disponivel() error {
	_, err := o.rodar(context.Background(), nil, o.c.Tesseract, "--version")
	if err != nil {
		return fmt.Errorf("tesseract indisponível (%s): %w", o.c.Tesseract, err)
	}
	return nil
}

type Palavra struct {
	Texto               string
	X, Y, W, H          int
	Bloco, Par, LinhaNo int
}

// Linha é uma linha de texto montada a partir das palavras, com a posição de cada uma.
type Linha struct {
	Texto    string
	Palavras []Palavra
	Ini      []int // início de cada palavra dentro de Texto
}

// Ler devolve a imagem decodificada e as linhas de texto encontradas nela.
func (o *OCR) Ler(dados []byte) (image.Image, []Linha, error) {
	img, _, err := image.Decode(bytes.NewReader(dados))
	if err != nil {
		return nil, nil, fmt.Errorf("imagem ilegível: %w", err)
	}
	// tela escura (tema escuro, terminal): inverte antes do OCR, que lê melhor preto no branco
	entrada := img
	if escura(img) {
		entrada = inverter(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, entrada); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout)
	defer cancel()
	out, err := o.rodar(ctx, buf.Bytes(), o.c.Tesseract, "stdin", "stdout", "-l", o.c.Idioma, "-c", "tessedit_do_invert=0", "tsv")
	if err != nil {
		return nil, nil, fmt.Errorf("tesseract: %w", err)
	}
	return img, linhas(parseTSV(out)), nil
}

func parseTSV(b []byte) []Palavra {
	var ps []Palavra
	for i, l := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue
		}
		c := strings.Split(l, "\t")
		if len(c) < 12 || c[0] != "5" || strings.TrimSpace(c[11]) == "" {
			continue
		}
		n := func(k int) int { v, _ := strconv.Atoi(c[k]); return v }
		ps = append(ps, Palavra{Texto: c[11], Bloco: n(2), Par: n(3), LinhaNo: n(4), X: n(6), Y: n(7), W: n(8), H: n(9)})
	}
	return ps
}

func linhas(ps []Palavra) []Linha {
	grupos := map[[3]int][]Palavra{}
	var ordem [][3]int
	for _, p := range ps {
		k := [3]int{p.Bloco, p.Par, p.LinhaNo}
		if _, ok := grupos[k]; !ok {
			ordem = append(ordem, k)
		}
		grupos[k] = append(grupos[k], p)
	}
	var out []Linha
	for _, k := range ordem {
		ws := grupos[k]
		sort.Slice(ws, func(i, j int) bool { return ws[i].X < ws[j].X })
		var l Linha
		for i, w := range ws {
			if i > 0 {
				l.Texto += " "
			}
			l.Ini = append(l.Ini, len(l.Texto))
			l.Texto += w.Texto
			l.Palavras = append(l.Palavras, w)
		}
		out = append(out, l)
	}
	return out
}

// Cobrir pinta retângulos pretos (com folga) sobre as caixas e devolve PNG.
func Cobrir(img image.Image, caixas []image.Rectangle) ([]byte, error) {
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	for _, r := range caixas {
		r = image.Rect(r.Min.X-3, r.Min.Y-3, r.Max.X+3, r.Max.Y+3).Intersect(b)
		draw.Draw(rgba, r, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, rgba)
	return buf.Bytes(), err
}

// TextoPDF extrai o texto de um PDF (vazio para PDF escaneado).
func (o *OCR) TextoPDF(pdf []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout)
	defer cancel()
	out, err := o.rodar(ctx, pdf, o.c.PDFToText, "-layout", "-", "-")
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(out), nil
}

// PaginasPDF renderiza as páginas do PDF como PNG (para OCR de PDF escaneado).
func (o *OCR) PaginasPDF(pdf []byte, maxPaginas int) ([][]byte, error) {
	dir, err := os.MkdirTemp("", "llm-dlp-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	arq := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(arq, pdf, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.c.Timeout*time.Duration(maxPaginas/10+1))
	defer cancel()
	if _, err := o.rodar(ctx, nil, o.c.PDFToPPM, "-r", "150", "-png", "-l", strconv.Itoa(maxPaginas+1), arq, filepath.Join(dir, "p")); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w", err)
	}
	nomes, _ := filepath.Glob(filepath.Join(dir, "p-*.png"))
	sort.Strings(nomes)
	if len(nomes) > maxPaginas {
		return nil, fmt.Errorf("PDF com mais de %d páginas", maxPaginas)
	}
	var pags [][]byte
	for _, n := range nomes {
		b, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		pags = append(pags, b)
	}
	return pags, nil
}

func (o *OCR) rodar(ctx context.Context, entrada []byte, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), o.c.Env...)
	if entrada != nil {
		cmd.Stdin = bytes.NewReader(entrada)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// escura: luminância média baixa (amostrada, para ser barato).
func escura(img image.Image) bool {
	b := img.Bounds()
	var soma, n uint64
	passo := max(1, b.Dx()/200)
	for y := b.Min.Y; y < b.Max.Y; y += passo {
		for x := b.Min.X; x < b.Max.X; x += passo {
			r, g, bl, _ := img.At(x, y).RGBA()
			soma += (299*uint64(r) + 587*uint64(g) + 114*uint64(bl)) / 1000
			n++
		}
	}
	return n > 0 && soma/n < 0x8000
}

func inverter(img image.Image) image.Image {
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

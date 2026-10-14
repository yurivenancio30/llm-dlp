package ocr

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"
)

// pngDeclaring: um PNG válido de 1x1 ponto com o cabeçalho trocado para declarar l x a pontos.
func pngDeclaring(t *testing.T, l, a uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// assinatura (8) + tamanho do IHDR (4) + "IHDR" (4) + largura (4) + altura (4) + ... + CRC (4)
	binary.BigEndian.PutUint32(b[16:], l)
	binary.BigEndian.PutUint32(b[20:], a)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

// Um arquivo de poucos bytes que declara uma imagem enorme é recusado pelo cabeçalho, antes de
// qualquer memória ser reservada para ela (decodificar derrubaria o processo).
func TestImageDeclaringHugeSize(t *testing.T) {
	o := New(Config{Tesseract: "/nao/existe"})
	for _, d := range [][2]uint32{{60000, 60000}, {sideMax + 1, 10}, {10, sideMax + 1}, {12000, 12000}} {
		_, _, err := o.Read(pngDeclaring(t, d[0], d[1]))
		if err == nil || !strings.Contains(err.Error(), "grande demais") {
			t.Errorf("%d x %d: devia recusar pelo tamanho, veio: %v", d[0], d[1], err)
		}
	}
	// a de tamanho normal passa do cabeçalho e só falha no tesseract (que aqui não existe)
	_, _, err := o.Read(pngDeclaring(t, 1, 1))
	if err == nil || strings.Contains(err.Error(), "grande demais") || !Missing(err) {
		t.Errorf("imagem normal: %v", err)
	}
}

// A saída do programa externo tem teto; as mensagens de erro dele são cortadas sem falhar.
func TestLimitedOutput(t *testing.T) {
	s := &limited{max: 10}
	if _, err := s.Write([]byte("12345678")); err != nil || s.overflowed {
		t.Fatal("dentro do teto não devia falhar")
	}
	if _, err := s.Write([]byte("12345678")); err == nil || !s.overflowed {
		t.Error("passou do teto e não falhou")
	}
	e := &limited{max: 10, discards: true}
	e.Write([]byte("12345678"))
	if n, err := e.Write([]byte("12345678")); err != nil || n != 8 || e.b.Len() != 8 {
		t.Errorf("o erro do programa devia ser cortado sem falhar: n=%d err=%v guardado=%d", n, err, e.b.Len())
	}
}

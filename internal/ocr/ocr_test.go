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

// pngDeclaring: a valid 1x1 PNG with the header changed to declare w x h pixels.
func pngDeclaring(t *testing.T, l, a uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// signature (8) + IHDR length (4) + "IHDR" (4) + width (4) + height (4) + ... + CRC (4)
	binary.BigEndian.PutUint32(b[16:], l)
	binary.BigEndian.PutUint32(b[20:], a)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

// A file of a few bytes that declares a huge image is refused by the header, before any memory is
// reserved for it (decoding would bring the process down).
func TestImageDeclaringHugeSize(t *testing.T) {
	o := New(Config{Tesseract: "/does/not/exist"})
	for _, d := range [][2]uint32{{60000, 60000}, {sideMax + 1, 10}, {10, sideMax + 1}, {12000, 12000}} {
		_, _, err := o.Read(pngDeclaring(t, d[0], d[1]))
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("%d x %d: should refuse by size, got: %v", d[0], d[1], err)
		}
	}
	// the normal-size one passes the header and only fails at tesseract (which does not exist here)
	_, _, err := o.Read(pngDeclaring(t, 1, 1))
	if err == nil || strings.Contains(err.Error(), "too large") || !Missing(err) {
		t.Errorf("normal image: %v", err)
	}
}

// The external program's output has a ceiling; its error messages are cut without failing.
func TestLimitedOutput(t *testing.T) {
	s := &limited{max: 10}
	if _, err := s.Write([]byte("12345678")); err != nil || s.overflowed {
		t.Fatal("within the ceiling it should not fail")
	}
	if _, err := s.Write([]byte("12345678")); err == nil || !s.overflowed {
		t.Error("passed the ceiling and did not fail")
	}
	e := &limited{max: 10, discards: true}
	e.Write([]byte("12345678"))
	if n, err := e.Write([]byte("12345678")); err != nil || n != 8 || e.b.Len() != 8 {
		t.Errorf("the program's error should be cut without failing: n=%d err=%v kept=%d", n, err, e.b.Len())
	}
}

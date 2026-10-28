// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 11), B: 180, A: 255})
		}
	}
	buffer := &bytes.Buffer{}
	if err := png.Encode(buffer, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func TestInspectPNGReencodesAndStripsTrailingBytes(t *testing.T) {
	content := append(pngBytes(t, 8, 6), []byte("<script>alert(1)</script>")...)
	path := writeTestFile(t, "image.png", content)
	gate := make(chan struct{}, 1)
	inspection, normalized, err := inspectFile(context.Background(), path, KindImage, MaxImageBytes, gate)
	if err != nil {
		t.Fatalf("inspect png: %v", err)
	}
	if inspection.MIME != "image/png" || inspection.Width != 8 || inspection.Height != 6 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
	if normalized == "" || normalized == path {
		t.Fatal("expected a re-encoded image path")
	}
	defer func() { _ = os.Remove(normalized) }()
	stripped, err := os.ReadFile(normalized)
	if err != nil {
		t.Fatalf("read normalized: %v", err)
	}
	if bytes.Contains(stripped, []byte("<script>")) {
		t.Fatal("trailing payload survived image normalization")
	}
}

func TestInspectRejectsSVG(t *testing.T) {
	path := writeTestFile(t, "image.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	gate := make(chan struct{}, 1)
	_, _, err := inspectFile(context.Background(), path, KindImage, MaxImageBytes, gate)
	if err != ErrUnsupportedType {
		t.Fatalf("SVG error = %v, want ErrUnsupportedType", err)
	}
}

func TestInspectRejectsWrongDeclaredKind(t *testing.T) {
	jpegBuffer := &bytes.Buffer{}
	if err := jpeg.Encode(jpegBuffer, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	path := writeTestFile(t, "sound.mp3", jpegBuffer.Bytes())
	gate := make(chan struct{}, 1)
	if _, _, err := inspectFile(context.Background(), path, KindAudio, MaxAudioBytes, gate); err != ErrUnsupportedType {
		t.Fatalf("audio inspect of JPEG error = %v, want ErrUnsupportedType", err)
	}
	if _, _, err := inspectFile(context.Background(), path, KindVideo, MaxVideoBytes, gate); err != ErrUnsupportedType {
		t.Fatalf("video inspect of JPEG error = %v, want ErrUnsupportedType", err)
	}
}

func TestSanitizeNameCannotEscapeStorage(t *testing.T) {
	for input, want := range map[string]string{
		"../../etc/passwd":       "passwd",
		`..\..\windows\system32`: "system32",
		"bad\x00name":            "badname",
		"":                       "asset",
		"normal.png":             "normal.png",
	} {
		if got := sanitizeName(input); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func glbBytes(t *testing.T, document string) []byte {
	t.Helper()
	jsonChunk := []byte(document)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	binChunk := []byte{0, 1, 2, 0}
	total := 12 + 8 + len(jsonChunk) + 8 + len(binChunk)
	out := make([]byte, 0, total)
	header := make([]byte, 12)
	copy(header[:4], "glTF")
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint32(header[8:12], uint32(total))
	out = append(out, header...)
	chunk := make([]byte, 8)
	binary.LittleEndian.PutUint32(chunk[:4], uint32(len(jsonChunk)))
	binary.LittleEndian.PutUint32(chunk[4:8], 0x4e4f534a)
	out = append(out, chunk...)
	out = append(out, jsonChunk...)
	binary.LittleEndian.PutUint32(chunk[:4], uint32(len(binChunk)))
	binary.LittleEndian.PutUint32(chunk[4:8], 0x004e4942)
	out = append(out, chunk...)
	out = append(out, binChunk...)
	return out
}

func TestValidateGLBRejectsExternalAndDataURIs(t *testing.T) {
	base := `{"asset":{"version":"2.0"},"buffers":[{"byteLength":4}],"accessors":[{"count":3}],"meshes":[{}]}`
	path := writeTestFile(t, "model.glb", glbBytes(t, base))
	if _, err := validateGLB(path, int64(len(glbBytes(t, base)))); err != nil {
		t.Fatalf("valid GLB rejected: %v", err)
	}
	for _, document := range []string{
		`{"asset":{"version":"2.0"},"buffers":[{"uri":"https://evil.example/model.bin"}]}`,
		`{"asset":{"version":"2.0"},"buffers":[{}],"images":[{"uri":"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="}]}`,
		`{"asset":{"version":"1.0"},"buffers":[{}]}`,
		`{"asset":{"version":"2.0"},"buffers":[{}],"accessors":[{"count":999999999}]}`,
	} {
		content := glbBytes(t, document)
		path := writeTestFile(t, "bad.glb", content)
		if _, err := validateGLB(path, int64(len(content))); err == nil {
			t.Fatalf("GLB was accepted: %s", document)
		}
	}
}

func TestValidateGLBRequiresEmbeddedBIN(t *testing.T) {
	document := `{"asset":{"version":"2.0"},"buffers":[{"byteLength":4}]}`
	jsonChunk := []byte(document)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	total := 12 + 8 + len(jsonChunk)
	out := make([]byte, 0, total)
	header := make([]byte, 12)
	copy(header[:4], "glTF")
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint32(header[8:12], uint32(total))
	out = append(out, header...)
	chunk := make([]byte, 8)
	binary.LittleEndian.PutUint32(chunk[:4], uint32(len(jsonChunk)))
	binary.LittleEndian.PutUint32(chunk[4:8], 0x4e4f534a)
	out = append(out, chunk...)
	out = append(out, jsonChunk...)
	path := writeTestFile(t, "nobin.glb", out)
	if _, err := validateGLB(path, int64(len(out))); err == nil {
		t.Fatal("GLB without embedded BIN was accepted")
	}
}

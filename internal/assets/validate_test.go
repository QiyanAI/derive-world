// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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

func buildTestGIF(logicalWidth, logicalHeight, frames, frameWidth, frameHeight int) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("GIF89a")
	descriptor := make([]byte, 7)
	binary.LittleEndian.PutUint16(descriptor[0:2], uint16(logicalWidth))
	binary.LittleEndian.PutUint16(descriptor[2:4], uint16(logicalHeight))
	buffer.Write(descriptor)
	for index := 0; index < frames; index++ {
		buffer.WriteByte(0x2C)
		imageDescriptor := make([]byte, 9)
		binary.LittleEndian.PutUint16(imageDescriptor[4:6], uint16(frameWidth))
		binary.LittleEndian.PutUint16(imageDescriptor[6:8], uint16(frameHeight))
		buffer.Write(imageDescriptor)
		buffer.WriteByte(2)
		buffer.WriteByte(1)
		buffer.WriteByte(0)
		buffer.WriteByte(0)
	}
	buffer.WriteByte(0x3B)
	return buffer.Bytes()
}

func TestGIFStructureRejectsTooManyFramesBeforeDecoding(t *testing.T) {
	path := writeTestFile(t, "many-frames.gif",
		buildTestGIF(16, 16, maxGIFFrames+1, 16, 16))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := checkGIFStructure(file); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("checkGIFStructure = %v, want ErrInvalidContent", err)
	}
}

func TestGIFStructureRejectsExcessiveTotalPixelsBeforeDecoding(t *testing.T) {
	path := writeTestFile(t, "many-pixels.gif",
		buildTestGIF(4000, 4000, 5, 4000, 4000))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := checkGIFStructure(file); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("checkGIFStructure = %v, want ErrInvalidContent", err)
	}
}

func TestGIFStructureAcceptsSmallAnimation(t *testing.T) {
	path := writeTestFile(t, "small.gif",
		buildTestGIF(16, 16, 2, 16, 16))
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := checkGIFStructure(file); err != nil {
		t.Fatalf("checkGIFStructure = %v, want nil", err)
	}
}

func TestGLBRejectsSecondJSONChunk(t *testing.T) {
	first := []byte(`{"asset":{"version":"2.0"}}`)
	second := []byte(`{"buffers":[{"uri":"https://evil.example/x.bin"}]}`)
	total := 12 + 8 + len(first) + 8 + len(second)
	buffer := make([]byte, 0, total)
	header := make([]byte, 12)
	copy(header[0:4], "glTF")
	binary.LittleEndian.PutUint32(header[4:8], 2)
	binary.LittleEndian.PutUint32(header[8:12], uint32(total))
	buffer = append(buffer, header...)
	chunk := make([]byte, 8)
	binary.LittleEndian.PutUint32(chunk[0:4], uint32(len(first)))
	binary.LittleEndian.PutUint32(chunk[4:8], 0x4e4f534a)
	buffer = append(buffer, chunk...)
	buffer = append(buffer, first...)
	binary.LittleEndian.PutUint32(chunk[0:4], uint32(len(second)))
	buffer = append(buffer, chunk...)
	buffer = append(buffer, second...)

	path := writeTestFile(t, "second-json.glb", buffer)
	if _, err := validateGLB(path, int64(len(buffer))); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("validateGLB = %v, want ErrInvalidContent", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"
)

const (
	maxImageDimension = 8192
	maxImagePixels    = 16_000_000
	maxGIFFrames      = 240
	maxGIFPixels      = 64_000_000
	maxGLBJSONBytes   = 1 << 20
	maxGLBAccessors   = 1024
	maxGLBVertices    = 3_000_000
)

type inspection struct {
	MIME   string
	Width  int
	Height int
	Size   int64
}

// inspectFile validates one temp file and, for images, writes a stripped
// replacement file. The caller owns both paths: a non-empty normalized path
// must replace the original temp file before storage.
func inspectFile(ctx context.Context, path string, kind Kind, limit int64,
	imageGate chan struct{}) (inspection, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return inspection{}, "", ErrInvalidContent
	}
	if info.Size() <= 0 || info.Size() > limit {
		if info.Size() > limit {
			return inspection{}, "", ErrTooLarge
		}
		return inspection{}, "", ErrInvalidContent
	}
	header := make([]byte, 64)
	file, err := os.Open(path)
	if err != nil {
		return inspection{}, "", ErrUnavailable
	}
	read, readErr := io.ReadFull(file, header)
	_ = file.Close()
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return inspection{}, "", ErrInvalidContent
	}
	header = header[:read]

	switch kind {
	case KindImage:
		return normalizeImage(ctx, path, limit, imageGate)
	case KindVideo:
		mime, ok := sniffVideo(header)
		if !ok {
			return inspection{}, "", ErrUnsupportedType
		}
		return inspection{MIME: mime, Size: info.Size()}, "", nil
	case KindAudio:
		mime, ok := sniffAudio(header)
		if !ok {
			return inspection{}, "", ErrUnsupportedType
		}
		return inspection{MIME: mime, Size: info.Size()}, "", nil
	case KindModel:
		mime, err := validateGLB(path, info.Size())
		if err != nil {
			return inspection{}, "", err
		}
		return inspection{MIME: mime, Size: info.Size()}, "", nil
	default:
		return inspection{}, "", ErrInvalidKind
	}
}

// normalizeImage decodes a PNG, JPEG or GIF, enforces dimension and
// decompression limits, and re-encodes it. Re-encoding strips EXIF, text
// chunks and trailing payloads, so the stored bytes contain only the image the
// browser will render. A malformed or unsupported image is rejected.
func normalizeImage(ctx context.Context, path string, limit int64,
	gate chan struct{}) (inspection, string, error) {
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return inspection{}, "", ctx.Err()
	}

	file, err := os.Open(path)
	if err != nil {
		return inspection{}, "", ErrUnavailable
	}
	config, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil {
		return inspection{}, "", ErrUnsupportedType
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxImageDimension || config.Height > maxImageDimension ||
		int64(config.Width)*int64(config.Height) > maxImagePixels {
		return inspection{}, "", ErrInvalidContent
	}
	if format != "png" && format != "jpeg" && format != "gif" {
		return inspection{}, "", ErrUnsupportedType
	}

	file, err = os.Open(path)
	if err != nil {
		return inspection{}, "", ErrUnavailable
	}
	defer func() { _ = file.Close() }()

	normalized, err := os.CreateTemp(filepathDir(path), "normalized-*")
	if err != nil {
		return inspection{}, "", ErrUnavailable
	}
	normalizedPath := normalized.Name()
	success := false
	defer func() {
		_ = normalized.Close()
		if !success {
			_ = os.Remove(normalizedPath)
		}
	}()

	var width, height int
	var mime string
	switch format {
	case "png":
		decoded, err := png.Decode(file)
		if err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		width, height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
		if err := png.Encode(normalized, decoded); err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		mime = "image/png"
	case "jpeg":
		decoded, err := jpeg.Decode(file)
		if err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		width, height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
		if err := jpeg.Encode(normalized, decoded, &jpeg.Options{Quality: 90}); err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		mime = "image/jpeg"
	case "gif":
		decoded, err := gif.DecodeAll(file)
		if err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		if len(decoded.Image) == 0 || len(decoded.Image) > maxGIFFrames {
			return inspection{}, "", ErrInvalidContent
		}
		totalPixels := 0
		for _, frame := range decoded.Image {
			totalPixels += frame.Bounds().Dx() * frame.Bounds().Dy()
			if totalPixels > maxGIFPixels {
				return inspection{}, "", ErrInvalidContent
			}
		}
		width, height = decoded.Config.Width, decoded.Config.Height
		if err := gif.EncodeAll(normalized, decoded); err != nil {
			return inspection{}, "", ErrInvalidContent
		}
		mime = "image/gif"
	default:
		return inspection{}, "", ErrUnsupportedType
	}

	if err := normalized.Sync(); err != nil {
		return inspection{}, "", ErrUnavailable
	}
	if err := normalized.Close(); err != nil {
		return inspection{}, "", ErrUnavailable
	}
	info, err := os.Stat(normalizedPath)
	if err != nil {
		return inspection{}, "", ErrUnavailable
	}
	if info.Size() <= 0 {
		return inspection{}, "", ErrInvalidContent
	}
	if info.Size() > limit {
		return inspection{}, "", ErrTooLarge
	}
	if width <= 0 || height <= 0 || width > maxImageDimension || height > maxImageDimension ||
		int64(width)*int64(height) > maxImagePixels {
		return inspection{}, "", ErrInvalidContent
	}
	success = true
	return inspection{MIME: mime, Width: width, Height: height, Size: info.Size()}, normalizedPath, nil
}

func sniffVideo(header []byte) (string, bool) {
	if len(header) >= 4 && string(header[:4]) == "\x1a\x45\xdf\xa3" {
		return "video/webm", true
	}
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		brand := strings.TrimRight(string(header[8:12]), "\x00 ")
		switch brand {
		case "isom", "iso2", "iso4", "iso5", "iso6", "mp41", "mp42",
			"avc1", "M4V ", "M4VH", "M4VP", "dash", "msdh", "msix":
			return "video/mp4", true
		}
	}
	return "", false
}

func sniffAudio(header []byte) (string, bool) {
	if len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
		return "audio/wav", true
	}
	if len(header) >= 4 && string(header[:4]) == "OggS" {
		return "audio/ogg", true
	}
	if len(header) >= 3 && string(header[:3]) == "ID3" {
		return "audio/mpeg", true
	}
	if len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0 {
		return "audio/mpeg", true
	}
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		brand := strings.TrimRight(string(header[8:12]), "\x00 ")
		switch brand {
		case "M4A ", "M4B ", "mp42", "isom", "iso2", "dash":
			return "audio/mp4", true
		}
	}
	return "", false
}

// validateGLB accepts only binary glTF 2.x with a bounded JSON chunk and no
// external resource URIs. Embedded data URIs are allowed; a model can never
// make the browser fetch an attacker-chosen host.
func validateGLB(path string, size int64) (string, error) {
	if size < 12 || size > MaxModelBytes {
		return "", ErrInvalidContent
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = file.Close() }()

	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", ErrInvalidContent
	}
	if string(header[:4]) != "glTF" {
		return "", ErrUnsupportedType
	}
	if version := binary.LittleEndian.Uint32(header[4:8]); version != 2 {
		return "", ErrUnsupportedType
	}
	if declared := binary.LittleEndian.Uint32(header[8:12]); int64(declared) != size {
		return "", ErrInvalidContent
	}

	chunkHeader := make([]byte, 8)
	if _, err := io.ReadFull(file, chunkHeader); err != nil {
		return "", ErrInvalidContent
	}
	chunkLength := binary.LittleEndian.Uint32(chunkHeader[:4])
	chunkType := binary.LittleEndian.Uint32(chunkHeader[4:8])
	if chunkType != 0x4e4f534a || chunkLength == 0 || chunkLength > maxGLBJSONBytes ||
		int64(chunkLength) > size-20 {
		return "", ErrInvalidContent
	}
	jsonChunk := make([]byte, chunkLength)
	if _, err := io.ReadFull(file, jsonChunk); err != nil {
		return "", ErrInvalidContent
	}
	var document struct {
		Asset struct {
			Version string `json:"version"`
		} `json:"asset"`
		Buffers []struct {
			URI string `json:"uri"`
		} `json:"buffers"`
		Images []struct {
			URI string `json:"uri"`
		} `json:"images"`
		Accessors []struct {
			Count int `json:"count"`
		} `json:"accessors"`
		Meshes     []json.RawMessage `json:"meshes"`
		Nodes      []json.RawMessage `json:"nodes"`
		Textures   []json.RawMessage `json:"textures"`
		Materials  []json.RawMessage `json:"materials"`
		Animations []json.RawMessage `json:"animations"`
		Skins      []json.RawMessage `json:"skins"`
	}
	if err := json.Unmarshal(jsonChunk, &document); err != nil {
		return "", ErrInvalidContent
	}
	if !strings.HasPrefix(document.Asset.Version, "2.") {
		return "", ErrUnsupportedType
	}
	// A GLB may never reach the network or smuggle an alternate URI scheme,
	// including data: URIs that can carry image payloads. Buffers and images
	// must live in the embedded BIN chunk.
	for _, buffer := range document.Buffers {
		if buffer.URI != "" {
			return "", ErrInvalidContent
		}
	}
	for _, image := range document.Images {
		if image.URI != "" {
			return "", ErrInvalidContent
		}
	}
	if len(document.Meshes) > 256 || len(document.Nodes) > 512 ||
		len(document.Images) > 64 || len(document.Textures) > 64 ||
		len(document.Materials) > 128 || len(document.Animations) > 32 ||
		len(document.Skins) > 64 {
		return "", ErrInvalidContent
	}
	if len(document.Accessors) > maxGLBAccessors {
		return "", ErrInvalidContent
	}
	total := 0
	for _, accessor := range document.Accessors {
		if accessor.Count < 0 {
			return "", ErrInvalidContent
		}
		total += accessor.Count
		if total > maxGLBVertices {
			return "", ErrInvalidContent
		}
	}
	// Walk the remaining chunks: every chunk must fit, and any declared buffer
	// requires the embedded BIN chunk GLTFLoader will reference.
	remaining := size - 20 - int64(chunkLength)
	hasBIN := false
	for remaining > 0 {
		if remaining < 8 {
			return "", ErrInvalidContent
		}
		header := make([]byte, 8)
		if _, err := io.ReadFull(file, header); err != nil {
			return "", ErrInvalidContent
		}
		length := int64(binary.LittleEndian.Uint32(header[:4]))
		chunkType := binary.LittleEndian.Uint32(header[4:8])
		if length < 0 || length > remaining-8 {
			return "", ErrInvalidContent
		}
		if chunkType == 0x004e4942 {
			hasBIN = true
		}
		if _, err := file.Seek(length, io.SeekCurrent); err != nil {
			return "", ErrInvalidContent
		}
		remaining -= 8 + length
	}
	if len(document.Buffers) > 0 && !hasBIN {
		return "", ErrInvalidContent
	}
	return "model/gltf-binary", nil
}

func filepathDir(value string) string {
	if index := strings.LastIndexAny(value, `/\`); index >= 0 {
		return value[:index]
	}
	return "."
}

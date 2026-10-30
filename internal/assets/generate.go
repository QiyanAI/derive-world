// SPDX-License-Identifier: Apache-2.0
package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/google/uuid"
)

// ErrGenerationUnavailable marks a request for a generator that is not
// configured or does not support the requested kind.
var ErrGenerationUnavailable = errors.New("asset generation unavailable")

// ProceduralGenerator is the first server-side asset provider. It is a
// deterministic, bounded image provider: it proves the generation seam without
// becoming a model call or a per-world code path. Future providers can replace
// it through Service.SetGenerator.
type ProceduralGenerator struct{}

func (ProceduralGenerator) Name() string { return "procedural" }

func (ProceduralGenerator) Kinds() []Kind { return []Kind{KindImage} }

func (ProceduralGenerator) Generate(_ context.Context, request GenerateRequest) (Generated, error) {
	if request.Kind != KindImage {
		return Generated{}, ErrGenerationUnavailable
	}
	width, height := 960, 540
	hue := float64(222)
	hue2 := float64(265)
	lightness := float64(14)
	starCount := 90
	seed := 7
	if request.Params != nil {
		if value, ok := numberParam(request.Params["hue"], 0, 359); ok {
			hue = value
		}
		if value, ok := numberParam(request.Params["hue2"], 0, 359); ok {
			hue2 = value
		}
		if value, ok := numberParam(request.Params["lightness"], 4, 40); ok {
			lightness = value
		}
		if value, ok := numberParam(request.Params["stars"], 0, 240); ok {
			starCount = int(value)
		}
		if value, ok := numberParam(request.Params["seed"], 0, 100000); ok {
			seed = int(value)
		}
	}

	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		ratio := float64(y) / float64(height-1)
		rowHue := hue + (hue2-hue)*ratio
		rowLight := lightness + (1-ratio)*6
		foreground := colorFromHSL(rowHue, 0.52, rowLight)
		for x := 0; x < width; x++ {
			source.SetRGBA(x, y, foreground)
		}
	}
	// A deterministic star field. The same request produces the same image.
	state := uint64(seed*7919 + 17)
	next := func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		return float64((state>>33)&0xffffff) / float64(0xffffff)
	}
	for index := 0; index < starCount; index++ {
		x := int(next() * float64(width))
		y := int(next() * float64(height))
		radius := 1 + int(next()*2)
		alpha := 0.28 + next()*0.55
		star := colorFromHSL(hue+((next()-0.5)*80), 0.7, 82)
		star.A = uint8(math.Round(alpha * 255))
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx*dx+dy*dy > radius*radius {
					continue
				}
				source.SetRGBA(x+dx, y+dy, blend(source.RGBAAt(x+dx, y+dy), star))
			}
		}
	}
	buffer := &bytes.Buffer{}
	if err := png.Encode(buffer, source); err != nil {
		return Generated{}, fmt.Errorf("assets: generate png: %w", err)
	}
	return Generated{
		Bytes: buffer.Bytes(), MIME: "image/png",
		Width: width, Height: height,
		Name: "generated-backdrop.png",
	}, nil
}

// Generate invokes the configured provider and sends its bytes through the
// same validation, quota, authorization and audit path as an upload.
func (s *Service) Generate(ctx context.Context, actorID, objectID uuid.UUID,
	request GenerateRequest, requestID string, ipHash []byte) (*Asset, error) {
	if s.generator == nil {
		return nil, ErrGenerationUnavailable
	}
	if request.Provider != "" && request.Provider != s.generator.Name() {
		return nil, ErrGenerationUnavailable
	}
	supported := false
	for _, kind := range s.generator.Kinds() {
		if kind == request.Kind {
			supported = true
			break
		}
	}
	if !supported {
		return nil, ErrGenerationUnavailable
	}
	generated, err := s.generator.Generate(ctx, request)
	if err != nil {
		return nil, err
	}
	name := generated.Name
	if name == "" {
		name = "generated-" + string(request.Kind)
	}
	return s.Upload(ctx, actorID, objectID, request.Kind, name,
		bytes.NewReader(generated.Bytes), requestID, ipHash)
}

func numberParam(value any, minimum, maximum float64) (float64, bool) {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) ||
		number < minimum || number > maximum {
		return 0, false
	}
	return number, true
}

func colorFromHSL(hue, saturation, lightness float64) color.RGBA {
	hue = math.Mod(hue, 360)
	if hue < 0 {
		hue += 360
	}
	h := hue / 360
	s := math.Max(0, math.Min(1, saturation))
	l := math.Max(0, math.Min(1, lightness/100))
	if l > 1 {
		l = 1
	}
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hueChannel(p, q, h+1.0/3)
		g = hueChannel(p, q, h)
		b = hueChannel(p, q, h-1.0/3)
	}
	return color.RGBA{
		R: uint8(math.Round(r * 255)), G: uint8(math.Round(g * 255)),
		B: uint8(math.Round(b * 255)), A: 255,
	}
}

func hueChannel(p, q, t float64) float64 {
	t = math.Mod(t, 1)
	if t < 0 {
		t += 1
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 0.5:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

func blend(base, overlay color.RGBA) color.RGBA {
	alpha := float64(overlay.A) / 255
	return color.RGBA{
		R: uint8(math.Round(float64(base.R)*(1-alpha) + float64(overlay.R)*alpha)),
		G: uint8(math.Round(float64(base.G)*(1-alpha) + float64(overlay.G)*alpha)),
		B: uint8(math.Round(float64(base.B)*(1-alpha) + float64(overlay.B)*alpha)),
		A: 255,
	}
}

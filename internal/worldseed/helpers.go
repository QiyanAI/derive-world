// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"encoding/json"
	"math"

	"github.com/QiyanAI/derive-world/internal/objects"
)

func zeroValue(fieldType string, index int) any {
	switch fieldType {
	case objects.OpenWorldFieldNumber:
		return float64(0)
	case objects.OpenWorldFieldFlag:
		return false
	default:
		_ = index
		return ""
	}
}

func layoutPosition(index, total, kindIndex int, mode string) (float64, float64) {
	if total <= 1 {
		return 800, 500
	}
	spread := 1.0
	if mode == "live" {
		spread = 1.18
	}
	angle := float64(index)*2*math.Pi/float64(total) + float64(kindIndex)*0.73
	radius := (170 + float64(index%3)*110) * spread
	x := 800 + math.Cos(angle)*radius*1.18
	y := 500 + math.Sin(angle)*radius*0.66
	return clampFloat(x, 80, 1520), clampFloat(y, 90, 900)
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func classicShape(index int) string {
	shapes := []string{
		objects.OpenWorldShapeOrb, objects.OpenWorldShapeBeacon,
		objects.OpenWorldShapeBloom, objects.OpenWorldShapeShard,
		objects.OpenWorldShapeRing, objects.OpenWorldShapeWave,
		objects.OpenWorldShapeGlyph, objects.OpenWorldShapeThread,
	}
	return shapes[index%len(shapes)]
}

func classicMotion(motion string) string {
	switch motion {
	case "none", "pulse", "float", "sway", "flicker", "breathe", "orbit":
		return motion
	default:
		return objects.OpenWorldMotionFloat
	}
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("worldseed: marshal composed props: " + err.Error())
	}
	return encoded
}

func isCJK(value string) bool {
	for _, r := range value {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

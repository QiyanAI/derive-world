// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// generatedForm produces a deterministic vector form from the author's own
// name for a kind. There is no fixed shape vocabulary here: the family and its
// parameters are derived from the name, so two different worlds do not inherit
// a shared palette of eight developer shapes. A model-supplied form always
// takes precedence.
func generatedForm(name string, index int) string {
	hash := stableHash(strings.ToLower(strings.TrimSpace(name)) + ":" + strconv.Itoa(index))
	family := hash % 6
	radius := 24 + float64(hash>>8%9)
	rotation := float64(hash>>16%360) * math.Pi / 180
	switch family {
	case 0:
		return circleForm(radius)
	case 1:
		return polygonForm(3+int(hash>>24%5), radius, rotation)
	case 2:
		return starForm(5+int(hash>>24%4), int(radius), 0.42+float64(hash>>32%20)/100)
	case 3:
		return petalForm(4+int(hash>>24%5), radius)
	case 4:
		return dropletForm(radius)
	default:
		return waveForm(radius)
	}
}

func generatedAvatar(name string) string {
	hash := stableHash("avatar:" + strings.ToLower(strings.TrimSpace(name)))
	radius := 13 + float64(hash%4)
	// A small walking figure: body, head and one forward arm, all in one path.
	body := circleForm(radius)
	head := fmt.Sprintf("M 0 %.2f A %.2f %.2f 0 1 1 0 %.2f A %.2f %.2f 0 1 1 0 %.2f Z",
		-radius*1.45, radius*0.42, radius*0.42, -radius*1.45+0.01,
		radius*0.42, radius*0.42, -radius*1.45+0.01)
	arm := fmt.Sprintf("M %.2f %.2f L %.2f %.2f L %.2f %.2f Z",
		radius*0.35, -radius*0.2, radius*0.95, -radius*0.55, radius*0.65, radius*0.05)
	return body + " " + head + " " + arm
}

func circleForm(radius float64) string {
	return fmt.Sprintf("M 0 %.2f A %.2f %.2f 0 1 1 0 %.2f A %.2f %.2f 0 1 1 0 %.2f Z",
		-radius, radius, radius, radius, radius, radius, -radius)
}

func polygonForm(points int, radius, rotation float64) string {
	if points < 3 {
		points = 3
	}
	var builder strings.Builder
	for index := 0; index < points; index++ {
		angle := rotation + float64(index)*2*math.Pi/float64(points)
		x := math.Cos(angle) * radius
		y := math.Sin(angle) * radius
		if index == 0 {
			fmt.Fprintf(&builder, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&builder, " L %.2f %.2f", x, y)
		}
	}
	builder.WriteString(" Z")
	return builder.String()
}

func starForm(points, radius int, innerRatio float64) string {
	if points < 3 {
		points = 3
	}
	var builder strings.Builder
	for index := 0; index < points*2; index++ {
		angle := -math.Pi/2 + float64(index)*math.Pi/float64(points)
		r := float64(radius)
		if index%2 == 1 {
			r *= innerRatio
		}
		x := math.Cos(angle) * r
		y := math.Sin(angle) * r
		if index == 0 {
			fmt.Fprintf(&builder, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&builder, " L %.2f %.2f", x, y)
		}
	}
	builder.WriteString(" Z")
	return builder.String()
}

func petalForm(petals int, radius float64) string {
	if petals < 3 {
		petals = 3
	}
	var builder strings.Builder
	for index := 0; index < petals; index++ {
		angle := float64(index) * 2 * math.Pi / float64(petals)
		tipX := math.Cos(angle) * radius
		tipY := math.Sin(angle) * radius
		leftAngle := angle - math.Pi/float64(petals)
		rightAngle := angle + math.Pi/float64(petals)
		controlRadius := radius * 0.95
		leftX := math.Cos(leftAngle) * controlRadius * 0.55
		leftY := math.Sin(leftAngle) * controlRadius * 0.55
		rightX := math.Cos(rightAngle) * controlRadius * 0.55
		rightY := math.Sin(rightAngle) * controlRadius * 0.55
		if index == 0 {
			fmt.Fprintf(&builder, "M 0 0 Q %.2f %.2f %.2f %.2f Q %.2f %.2f 0 0",
				leftX, leftY, tipX, tipY, rightX, rightY)
		} else {
			fmt.Fprintf(&builder, " Q %.2f %.2f %.2f %.2f Q %.2f %.2f 0 0",
				leftX, leftY, tipX, tipY, rightX, rightY)
		}
	}
	builder.WriteString(" Z")
	return builder.String()
}

func dropletForm(radius float64) string {
	return fmt.Sprintf("M 0 %.2f C %.2f %.2f %.2f %.2f 0 %.2f C %.2f %.2f %.2f %.2f 0 %.2f Z",
		-radius*1.5, radius*0.9, -radius*0.9, radius*0.9, radius*0.35,
		radius*0.7, -radius*0.9, radius*0.9, -radius*0.9, radius*0.9, -radius*1.5)
}

func waveForm(radius float64) string {
	return fmt.Sprintf("M %.2f 0 C %.2f %.2f %.2f %.2f 0 %.2f C %.2f %.2f %.2f %.2f %.2f 0 Z",
		-radius, -radius*0.9, -radius*0.85, -radius*0.4, -radius*0.7,
		radius*0.2,
		radius*0.4, radius*0.7, radius*0.9, radius*0.85, radius)
}

func stableHash(value string) uint64 {
	const offset = uint64(1469598103934665603)
	const prime = uint64(1099511628211)
	hash := offset
	for index := 0; index < len(value); index++ {
		hash ^= uint64(value[index])
		hash *= prime
	}
	return hash
}

// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/QiyanAI/derive-world/internal/objects"
)

// This file owns the bounded, lossless repairs the AI authoring path may apply
// to a model-authored seed. Every repair is deterministic and re-validated by
// the strict Decode path afterwards, so a repaired seed is exactly as safe as
// one the model got right on the first try. Authors that submit a seed through
// the public API still get the strict decoder; only Personal AI calls this.

var rawMessageType = reflect.TypeOf(json.RawMessage{})

// DecodeRepairing decodes one seed and, when the only problem is an
// unrecognized field name, removes that field and decodes again. The second
// decode still applies every type, range and enum check. It returns the paths
// of the removed fields so the caller can tell the human what was left out.
//
// A seed whose required content disappears with the removed fields is not
// repaired silently: the strict error is returned unchanged.
func DecodeRepairing(raw json.RawMessage) (*Brief, []string, error) {
	if brief, err := Decode(raw); err == nil {
		return brief, nil, nil
	} else if !isUnknownField(err) {
		return nil, nil, err
	}
	repaired, dropped := stripUnknownFields(raw)
	if len(dropped) == 0 {
		_, err := Decode(raw)
		return nil, nil, err
	}
	brief, err := Decode(repaired)
	if err != nil {
		return nil, nil, err
	}
	return brief, dropped, nil
}

// stripUnknownFields removes every object key that is not part of the seed
// schema. The walk follows the Go types, so the allow-list can never drift away
// from Brief. Free-form values (map[string]any, any, json.RawMessage such as
// experience.custom) are passed through untouched.
func stripUnknownFields(raw json.RawMessage) (json.RawMessage, []string) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return raw, nil
	}
	dropped := make([]string, 0, 4)
	repaired := stripUnknownValue(document, reflect.TypeOf(Brief{}), "$", &dropped)
	encoded, err := json.Marshal(repaired)
	if err != nil {
		return raw, nil
	}
	return encoded, dropped
}

func stripUnknownValue(value any, typ reflect.Type, path string, dropped *[]string) any {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == rawMessageType {
		return value
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		fields := seedFields(typ)
		repaired := make(map[string]any, len(object))
		for key, item := range object {
			fieldType, known := fields[key]
			if !known {
				*dropped = append(*dropped, path+"."+key)
				continue
			}
			repaired[key] = stripUnknownValue(item, fieldType, path+"."+key, dropped)
		}
		return repaired
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return value
		}
		repaired := make([]any, len(items))
		for index, item := range items {
			repaired[index] = stripUnknownValue(item, typ.Elem(),
				fmt.Sprintf("%s[%d]", path, index), dropped)
		}
		return repaired
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		repaired := make(map[string]any, len(object))
		for key, item := range object {
			repaired[key] = stripUnknownValue(item, typ.Elem(), path+"."+key, dropped)
		}
		return repaired
	default:
		return value
	}
}

func seedFields(typ reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, typ.NumField())
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		fields[name] = field.Type
	}
	return fields
}

func isUnknownField(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown field")
}

// ClampBriefNumbers moves optional presentation numbers into the documented
// range instead of rejecting the whole plan. It never touches required fields,
// indexes, enum names or prose, and it never invents a value: an absent number
// stays absent. The same constants the validator enforces are used here, so a
// clamped seed always passes validateParticleSpec and friends.
func ClampBriefNumbers(raw json.RawMessage) (json.RawMessage, bool) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return raw, false
	}
	changed := false
	clamp := func(container map[string]any, key string, min, max float64, integer bool) {
		if container == nil {
			return
		}
		value, ok := container[key]
		if !ok {
			return
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return
		}
		clamped := math.Min(math.Max(number, min), max)
		if integer {
			clamped = math.Round(clamped)
		}
		if clamped != number {
			container[key] = clamped
			changed = true
		}
	}
	object := func(value any) map[string]any {
		result, _ := value.(map[string]any)
		return result
	}
	list := func(value any) []any {
		result, _ := value.([]any)
		return result
	}

	for _, kindValue := range list(document["kinds"]) {
		kind := object(kindValue)
		clamp(kind, "hue", 0, 359, true)
	}
	for _, thingValue := range list(document["things"]) {
		thing := object(thingValue)
		clamp(thing, "x", 0, 1, false)
		clamp(thing, "y", 0, 1, false)
	}
	experience := object(document["experience"])
	if experience != nil {
		live := object(experience["live"])
		if live != nil {
			clamp(live, "speed", minWalkSpeed, maxWalkSpeed, false)
			clamp(live, "timer", 0, 3600, false)
			particles := object(live["particles"])
			if particles != nil {
				clamp(particles, "count", 0, 120, true)
				clamp(particles, "hue", 0, 359, true)
				clamp(particles, "size", 1, 6, false)
				clamp(particles, "speed", 0.2, 3, false)
			}
			timeline := object(live["timeline"])
			if timeline != nil {
				clamp(timeline, "duration", 1, 120, false)
			}
		}
		scene := object(experience["scene3d"])
		if scene != nil {
			clamp(scene, "sky_hue", 0, 359, true)
			clamp(scene, "ground_hue", 0, 359, true)
			clamp(scene, "fog", 0, 0.2, false)
			clamp(scene, "distance", 4, 40, false)
			clamp(scene, "speed", 1, 10, false)
			clamp(scene, "gravity", 0, 40, false)
			for _, viewValue := range list(scene["views"]) {
				view := object(viewValue)
				clamp(view, "scale", 0.2, 10, false)
				clamp(view, "elevation", 0, 30, false)
			}
		}
		audio := object(experience["audio"])
		if audio != nil {
			clamp(audio, "volume", 0, 1, false)
			clampTone(object(audio["tone"]), clamp)
			for _, cueValue := range list(audio["cues"]) {
				cue := object(cueValue)
				clamp(cue, "volume", 0, 1, false)
				clampTone(object(cue["tone"]), clamp)
			}
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func clampTone(tone map[string]any, clamp func(map[string]any, string, float64, float64, bool)) {
	if tone == nil {
		return
	}
	clamp(tone, "frequency", 40, 8000, false)
	clamp(tone, "volume", 0, 0.5, false)
	clamp(tone, "pulse", 0, 4, false)
}

// CoerceInteractionValues fits only unambiguous JSON representations to the
// target field's declared type before the strict decoder runs: a numeric string
// for a number field, "true"/"false" for a flag field, a number or boolean for
// a text field, and a numeric string for an add operation. It never guesses a
// status value or toggle semantics, and the strict decoder still runs on the
// result, so an ambiguous interaction is rejected exactly as before.
func CoerceInteractionValues(raw json.RawMessage) (json.RawMessage, bool) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return raw, false
	}
	kinds := anyList(document["kinds"])
	things := anyList(document["things"])
	interactions := anyList(document["interactions"])
	if len(kinds) == 0 || len(things) == 0 || len(interactions) == 0 {
		return raw, false
	}
	changed := false
	for _, interactionValue := range interactions {
		interaction, ok := interactionValue.(map[string]any)
		if !ok {
			continue
		}
		op, _ := interaction["op"].(string)
		if op == objects.OpenWorldOpToggle {
			continue
		}
		targetIndex, ok := anyIndex(interaction["to"])
		if !ok {
			targetIndex, ok = anyIndex(interaction["from"])
		}
		if !ok || targetIndex < 0 || targetIndex >= len(things) {
			continue
		}
		thing, ok := things[targetIndex].(map[string]any)
		if !ok {
			continue
		}
		kindIndex, ok := anyIndex(thing["kind"])
		if !ok || kindIndex < 0 || kindIndex >= len(kinds) {
			continue
		}
		kind, ok := kinds[kindIndex].(map[string]any)
		if !ok {
			continue
		}
		fieldType := "status"
		label, _ := interaction["field"].(string)
		if label != "" && label != "status" {
			for _, fieldValue := range anyList(kind["fields"]) {
				field, ok := fieldValue.(map[string]any)
				if !ok {
					continue
				}
				if fieldLabel, _ := field["label"].(string); fieldLabel == label {
					fieldType, _ = field["type"].(string)
					break
				}
			}
		}
		value, present := interaction["value"]
		if !present {
			continue
		}
		switch op {
		case objects.OpenWorldOpSet:
			switch fieldType {
			case objects.OpenWorldFieldText:
				switch typed := value.(type) {
				case float64:
					interaction["value"] = strconv.FormatFloat(typed, 'f', -1, 64)
					changed = true
				case bool:
					interaction["value"] = strconv.FormatBool(typed)
					changed = true
				}
			case objects.OpenWorldFieldNumber:
				if text, ok := value.(string); ok {
					if number, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
						interaction["value"] = number
						changed = true
					}
				}
			case objects.OpenWorldFieldFlag:
				switch typed := value.(type) {
				case string:
					if flag, err := strconv.ParseBool(strings.TrimSpace(typed)); err == nil {
						interaction["value"] = flag
						changed = true
					}
				case float64:
					if typed == 0 || typed == 1 {
						interaction["value"] = typed == 1
						changed = true
					}
				}
			}
		case objects.OpenWorldOpAdd:
			if text, ok := value.(string); ok {
				if number, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
					interaction["value"] = number
					changed = true
				}
			}
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func anyList(value any) []any {
	list, _ := value.([]any)
	return list
}

// NormalizeVariableNames makes an opaque variable key satisfy the runtime's
// lowercase ASCII name rule without changing what it refers to. It renames the
// same original key consistently across every variable reference it can see
// (audio cues, live goal, media slots). A readable slug is used when one can be
// derived; otherwise a deterministic hash keeps the key stable. Names that are
// already valid are never touched.
func NormalizeVariableNames(raw json.RawMessage) (json.RawMessage, bool) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return raw, false
	}
	experience := mapValue(document["experience"])
	if experience == nil {
		return raw, false
	}
	type reference struct {
		container map[string]any
		key       string
	}
	references := make([]reference, 0, 4)
	add := func(container map[string]any, key string) {
		if container == nil {
			return
		}
		if text, ok := container[key].(string); ok && text != "" {
			references = append(references, reference{container: container, key: key})
		}
	}
	media := mapValue(experience["media"])
	add(media, "show_variable")
	add(media, "play_variable")
	live := mapValue(experience["live"])
	if live != nil {
		add(mapValue(live["goal"]), "variable")
	}
	audio := mapValue(experience["audio"])
	for _, cueValue := range anyList(audio["cues"]) {
		add(mapValue(cueValue), "variable")
	}
	if len(references) == 0 {
		return raw, false
	}
	used := make(map[string]bool, len(references))
	for _, reference := range references {
		if name, ok := reference.container[reference.key].(string); ok && validVariableName(name) {
			used[name] = true
		}
	}
	mapping := make(map[string]string, len(references))
	for _, reference := range references {
		name, _ := reference.container[reference.key].(string)
		if name == "" || validVariableName(name) {
			continue
		}
		if _, done := mapping[name]; done {
			continue
		}
		candidate := variableSlug(name)
		if candidate == "" || used[candidate] {
			candidate = variableHash(name)
		}
		if used[candidate] {
			continue
		}
		mapping[name] = candidate
		used[candidate] = true
	}
	if len(mapping) == 0 {
		return raw, false
	}
	changed := false
	for _, reference := range references {
		name, _ := reference.container[reference.key].(string)
		if replacement, ok := mapping[name]; ok {
			reference.container[reference.key] = replacement
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func mapValue(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func variableSlug(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			builder.WriteRune(r)
			lastUnderscore = r == '_'
		default:
			if !lastUnderscore && builder.Len() > 0 {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "_-.")
	if slug == "" {
		return ""
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		slug = "v_" + slug
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if !validVariableName(slug) {
		return ""
	}
	return slug
}

func variableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("v_%x", sum[:6])
}

func anyIndex(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) {
		return 0, false
	}
	return int(number), true
}

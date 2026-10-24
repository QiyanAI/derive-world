// SPDX-License-Identifier: Apache-2.0

// Package capability owns capability definitions, the single Capability
// Broker, the trusted-core providers, and the external Provider HTTP client.
// It never writes object state and never makes object authorization decisions
// outside the Trust Kernel and the objects domain service.
package capability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// schemaDialect is the only accepted JSON Schema dialect.
const schemaDialect = "https://json-schema.org/draft/2020-12/schema"

// ErrSchemaInvalid reports a malformed or unsupported schema document.
var ErrSchemaInvalid = errors.New("invalid capability schema")

// ValidationError carries the failing instance location as a JSON pointer.
type ValidationError struct {
	Pointer string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Pointer + ": " + e.Message
}

var uuidAnyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Schema is a compiled, self-contained JSON Schema 2020-12 subset. The subset
// is closed: any keyword outside the supported set makes CompileSchema fail,
// so a future schema edit cannot silently weaken runtime validation.
type Schema struct {
	raw     json.RawMessage
	root    *schemaNode
	defs    map[string]*schemaNode
	rootID  string
	rootSch string
}

type schemaNode struct {
	types                string
	properties           map[string]*schemaNode
	required             []string
	additionalProperties *bool
	items                *schemaNode
	minItems             *int
	maxItems             *int
	uniqueItems          bool
	minLength            *int
	maxLength            *int
	minimum              *json.Number
	maximum              *json.Number
	enum                 []any
	constValue           any
	hasConst             bool
	pattern              *regexp.Regexp
	format               string
	ref                  string
}

var allowedSchemaKeys = map[string]bool{
	"$schema": true, "$id": true, "$defs": true, "$ref": true,
	"type": true, "properties": true, "additionalProperties": true,
	"required": true, "items": true, "minItems": true, "maxItems": true,
	"uniqueItems": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "enum": true, "const": true,
	"pattern": true, "format": true,
}

// CompileSchema parses and validates a schema document. The document must be
// rooted in an object with additionalProperties=false.
func CompileSchema(raw []byte) (*Schema, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: root must be an object", ErrSchemaInvalid)
	}
	dialect, _ := root["$schema"].(string)
	if dialect != schemaDialect {
		return nil, fmt.Errorf("%w: $schema must be %s", ErrSchemaInvalid, schemaDialect)
	}
	rootID, _ := root["$id"].(string)
	schema := &Schema{raw: append(json.RawMessage(nil), raw...), defs: map[string]*schemaNode{},
		rootID: rootID, rootSch: dialect}
	defs, err := schema.compileDefs(root)
	if err != nil {
		return nil, err
	}
	schema.defs = defs
	node, err := schema.compileNode(root, true)
	if err != nil {
		return nil, err
	}
	schema.root = node
	if node.types != "object" {
		return nil, fmt.Errorf("%w: root type must be object", ErrSchemaInvalid)
	}
	if node.additionalProperties == nil || *node.additionalProperties {
		return nil, fmt.Errorf("%w: root additionalProperties must be false", ErrSchemaInvalid)
	}
	return schema, nil
}

func (s *Schema) compileDefs(root map[string]any) (map[string]*schemaNode, error) {
	rawDefs, ok := root["$defs"]
	if !ok {
		return map[string]*schemaNode{}, nil
	}
	defsObject, ok := rawDefs.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: $defs must be an object", ErrSchemaInvalid)
	}
	defs := make(map[string]*schemaNode, len(defsObject))
	for name, value := range defsObject {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: $defs/%s must be an object", ErrSchemaInvalid, name)
		}
		node, err := s.compileNode(object, false)
		if err != nil {
			return nil, fmt.Errorf("$defs/%s: %w", name, err)
		}
		defs[name] = node
	}
	return defs, nil
}

func (s *Schema) compileNode(object map[string]any, root bool) (*schemaNode, error) {
	for key := range object {
		if !allowedSchemaKeys[key] {
			return nil, fmt.Errorf("%w: unsupported keyword %q", ErrSchemaInvalid, key)
		}
		if (key == "$schema" || key == "$id" || key == "$defs") && !root {
			return nil, fmt.Errorf("%w: %s is only allowed at the root", ErrSchemaInvalid, key)
		}
	}
	node := &schemaNode{}
	if rawType, ok := object["type"]; ok {
		if _, isArray := rawType.([]any); isArray {
			return nil, fmt.Errorf("%w: type arrays are not supported", ErrSchemaInvalid)
		}
		value, ok := rawType.(string)
		if !ok {
			return nil, fmt.Errorf("%w: type must be a string", ErrSchemaInvalid)
		}
		switch value {
		case "object", "array", "string", "integer", "number", "boolean", "null":
			node.types = value
		default:
			return nil, fmt.Errorf("%w: unknown type %q", ErrSchemaInvalid, value)
		}
	}
	if rawRef, ok := object["$ref"]; ok {
		ref, ok := rawRef.(string)
		if !ok || !strings.HasPrefix(ref, "#/$defs/") || len(ref) <= len("#/$defs/") {
			return nil, fmt.Errorf("%w: only local #/$defs references are supported", ErrSchemaInvalid)
		}
		node.ref = strings.TrimPrefix(ref, "#/$defs/")
		delete(object, "$ref")
		if len(object) != 0 {
			return nil, fmt.Errorf("%w: $ref cannot have sibling keywords", ErrSchemaInvalid)
		}
		return node, nil
	}
	if rawProps, ok := object["properties"]; ok {
		props, ok := rawProps.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: properties must be an object", ErrSchemaInvalid)
		}
		node.properties = make(map[string]*schemaNode, len(props))
		for name, value := range props {
			child, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: properties/%s must be an object", ErrSchemaInvalid, name)
			}
			compiled, err := s.compileNode(child, false)
			if err != nil {
				return nil, fmt.Errorf("properties/%s: %w", name, err)
			}
			node.properties[name] = compiled
		}
	}
	if rawAdditional, ok := object["additionalProperties"]; ok {
		value, ok := rawAdditional.(bool)
		if !ok {
			return nil, fmt.Errorf("%w: additionalProperties must be boolean", ErrSchemaInvalid)
		}
		node.additionalProperties = &value
	}
	if rawRequired, ok := object["required"]; ok {
		values, ok := rawRequired.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: required must be an array", ErrSchemaInvalid)
		}
		seen := map[string]bool{}
		for _, item := range values {
			name, ok := item.(string)
			if !ok || name == "" || seen[name] {
				return nil, fmt.Errorf("%w: required must contain unique non-empty strings", ErrSchemaInvalid)
			}
			seen[name] = true
			node.required = append(node.required, name)
		}
	}
	if rawItems, ok := object["items"]; ok {
		child, ok := rawItems.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: items must be an object", ErrSchemaInvalid)
		}
		compiled, err := s.compileNode(child, false)
		if err != nil {
			return nil, fmt.Errorf("items: %w", err)
		}
		node.items = compiled
	}
	for key, target := range map[string]**int{
		"minItems": &node.minItems, "maxItems": &node.maxItems,
		"minLength": &node.minLength, "maxLength": &node.maxLength,
	} {
		if raw, ok := object[key]; ok {
			value, err := nonNegativeInt(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: %s must be a non-negative integer", ErrSchemaInvalid, key)
			}
			*target = &value
		}
	}
	if raw, ok := object["uniqueItems"]; ok {
		value, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("%w: uniqueItems must be boolean", ErrSchemaInvalid)
		}
		node.uniqueItems = value
	}
	for key, target := range map[string]**json.Number{
		"minimum": &node.minimum, "maximum": &node.maximum,
	} {
		if raw, ok := object[key]; ok {
			number, ok := raw.(json.Number)
			if !ok {
				return nil, fmt.Errorf("%w: %s must be a number", ErrSchemaInvalid, key)
			}
			value := number
			*target = &value
		}
	}
	if raw, ok := object["enum"]; ok {
		values, ok := raw.([]any)
		if !ok || len(values) == 0 {
			return nil, fmt.Errorf("%w: enum must be a non-empty array", ErrSchemaInvalid)
		}
		node.enum = values
	}
	if raw, ok := object["const"]; ok {
		node.constValue = raw
		node.hasConst = true
	}
	if raw, ok := object["pattern"]; ok {
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%w: pattern must be a string", ErrSchemaInvalid)
		}
		compiled, err := regexp.Compile(value)
		if err != nil {
			return nil, fmt.Errorf("%w: pattern: %v", ErrSchemaInvalid, err)
		}
		node.pattern = compiled
	}
	if raw, ok := object["format"]; ok {
		value, ok := raw.(string)
		if !ok || (value != "uuid" && value != "date-time") {
			return nil, fmt.Errorf("%w: unsupported format", ErrSchemaInvalid)
		}
		node.format = value
	}
	if node.types == "" && node.ref == "" {
		return nil, fmt.Errorf("%w: schema node has no type or $ref", ErrSchemaInvalid)
	}
	return node, nil
}

// ValidateJSON checks one JSON document against the schema.
func (s *Schema) ValidateJSON(raw []byte) error {
	value, err := decodeJSON(raw)
	if err != nil {
		return &ValidationError{Pointer: "", Message: "invalid JSON: " + err.Error()}
	}
	return s.ValidateValue(value)
}

// ValidateValue checks a decoded JSON value against the schema.
func (s *Schema) ValidateValue(value any) error {
	return s.validate(s.root, value, "")
}

func (s *Schema) validate(node *schemaNode, value any, pointer string) error {
	if node.ref != "" {
		target, ok := s.defs[node.ref]
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "unresolved schema reference"}
		}
		node = target
	}
	if node.hasConst && !jsonEqual(value, node.constValue) {
		return &ValidationError{Pointer: pointer, Message: "value does not match const"}
	}
	if node.enum != nil {
		matched := false
		for _, candidate := range node.enum {
			if jsonEqual(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return &ValidationError{Pointer: pointer, Message: "value is not in enum"}
		}
	}
	switch node.types {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "expected object"}
		}
		for _, name := range node.required {
			if _, ok := object[name]; !ok {
				return &ValidationError{Pointer: pointer + "/" + name, Message: "required property is missing"}
			}
		}
		keys := make([]string, 0, len(object))
		for name := range object {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			child, known := node.properties[name]
			if !known {
				if node.additionalProperties != nil && !*node.additionalProperties {
					return &ValidationError{Pointer: pointer + "/" + name, Message: "additional property is not allowed"}
				}
				continue
			}
			if err := s.validate(child, object[name], pointer+"/"+name); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "expected array"}
		}
		if node.minItems != nil && len(array) < *node.minItems {
			return &ValidationError{Pointer: pointer, Message: "array is shorter than minItems"}
		}
		if node.maxItems != nil && len(array) > *node.maxItems {
			return &ValidationError{Pointer: pointer, Message: "array is longer than maxItems"}
		}
		if node.uniqueItems {
			seen := make(map[string]bool, len(array))
			for _, item := range array {
				key, err := canonicalKey(item)
				if err != nil {
					return &ValidationError{Pointer: pointer, Message: "cannot compare array items"}
				}
				if seen[key] {
					return &ValidationError{Pointer: pointer, Message: "array items are not unique"}
				}
				seen[key] = true
			}
		}
		if node.items != nil {
			for index, item := range array {
				if err := s.validate(node.items, item, pointer+"/"+strconv.Itoa(index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "expected string"}
		}
		length := utf8.RuneCountInString(text)
		if node.minLength != nil && length < *node.minLength {
			return &ValidationError{Pointer: pointer, Message: "string is shorter than minLength"}
		}
		if node.maxLength != nil && length > *node.maxLength {
			return &ValidationError{Pointer: pointer, Message: "string is longer than maxLength"}
		}
		if node.pattern != nil && !node.pattern.MatchString(text) {
			return &ValidationError{Pointer: pointer, Message: "string does not match pattern"}
		}
		if err := validateFormat(node.format, text); err != nil {
			return &ValidationError{Pointer: pointer, Message: err.Error()}
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "expected integer"}
		}
		if _, ok := integralNumber(number); !ok {
			return &ValidationError{Pointer: pointer, Message: "integer is out of range or not integral"}
		}
		if err := validateBounds(node, number); err != nil {
			return &ValidationError{Pointer: pointer, Message: err.Error()}
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return &ValidationError{Pointer: pointer, Message: "expected number"}
		}
		if err := validateBounds(node, number); err != nil {
			return &ValidationError{Pointer: pointer, Message: err.Error()}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return &ValidationError{Pointer: pointer, Message: "expected boolean"}
		}
	case "null":
		if value != nil {
			return &ValidationError{Pointer: pointer, Message: "expected null"}
		}
	default:
		return &ValidationError{Pointer: pointer, Message: "schema node has no type"}
	}
	return nil
}

func integralNumber(value json.Number) (int64, bool) {
	if parsed, err := strconv.ParseInt(value.String(), 10, 64); err == nil {
		return parsed, true
	}
	parsed, err := strconv.ParseFloat(value.String(), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed != math.Trunc(parsed) ||
		parsed < math.MinInt64 || parsed > math.MaxInt64 {
		return 0, false
	}
	return int64(parsed), true
}

func validateBounds(node *schemaNode, value json.Number) error {
	number, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		return errors.New("number is out of range")
	}
	if node.minimum != nil {
		min, err := strconv.ParseFloat(node.minimum.String(), 64)
		if err != nil {
			return errors.New("invalid minimum")
		}
		if number < min {
			return errors.New("number is below minimum")
		}
	}
	if node.maximum != nil {
		max, err := strconv.ParseFloat(node.maximum.String(), 64)
		if err != nil {
			return errors.New("invalid maximum")
		}
		if number > max {
			return errors.New("number is above maximum")
		}
	}
	return nil
}

func validateFormat(format, value string) error {
	switch format {
	case "":
		return nil
	case "uuid":
		if !uuidAnyPattern.MatchString(value) {
			return errors.New("string is not a UUID")
		}
		if _, err := uuid.Parse(value); err != nil {
			return errors.New("string is not a UUID")
		}
		return nil
	case "date-time":
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return errors.New("string is not an RFC 3339 date-time")
		}
		return nil
	default:
		return errors.New("unsupported format")
	}
}

func nonNegativeInt(value any) (int, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("not a number")
	}
	parsed, err := strconv.Atoi(number.String())
	if err != nil || parsed < 0 {
		return 0, errors.New("not a non-negative integer")
	}
	return parsed, nil
}

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, errors.New("more than one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return value, nil
}

func jsonEqual(left, right any) bool {
	leftKey, leftErr := canonicalKey(left)
	rightKey, rightErr := canonicalKey(right)
	return leftErr == nil && rightErr == nil && leftKey == rightKey
}

func canonicalKey(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Raw returns the original schema document.
func (s *Schema) Raw() json.RawMessage {
	return append(json.RawMessage(nil), s.raw...)
}

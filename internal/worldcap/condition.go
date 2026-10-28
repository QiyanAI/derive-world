// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// EvaluateCondition evaluates a validated condition against current state.
// Missing entities or fields read as false, so a condition is total over the
// declared field space. The evaluator is bounded by the same depth limit as
// validation and never calls into a capability.
func EvaluateCondition(condition *Condition, event Event, itemEntityID string, world WorldAccess) bool {
	return evaluateCondition(condition, event, itemEntityID, world, 1)
}

func evaluateCondition(condition *Condition, event Event, itemEntityID string, world WorldAccess, depth int) bool {
	if condition == nil || depth > maxConditionDepth+1 {
		return false
	}
	if condition.Not != nil {
		return !evaluateCondition(condition.Not, event, itemEntityID, world, depth+1)
	}
	if len(condition.All) > 0 {
		for index := range condition.All {
			if !evaluateCondition(&condition.All[index], event, itemEntityID, world, depth+1) {
				return false
			}
		}
		return true
	}
	if len(condition.Any) > 0 {
		for index := range condition.Any {
			if evaluateCondition(&condition.Any[index], event, itemEntityID, world, depth+1) {
				return true
			}
		}
		return false
	}
	entityID := condition.EntityID
	switch entityID {
	case "$event.entity":
		entityID = event.EntityID
	case "$item":
		entityID = itemEntityID
	}
	if entityID == "" {
		return false
	}
	target := condition.Value
	if isEventValue(target) {
		target = event.Value
	}
	if condition.Field == "status" {
		current, ok := world.Status(entityID)
		if !ok {
			return false
		}
		text, ok := target.(string)
		if !ok {
			return false
		}
		switch condition.Op {
		case "eq":
			return current == text
		case "ne":
			return current != text
		}
		return false
	}
	fieldType, ok := world.FieldType(entityID, condition.Field)
	if !ok {
		return false
	}
	current, ok := world.Value(entityID, condition.Field)
	if !ok {
		// A declared field that has never been written reads as its zero value,
		// just as it does in the legacy rule interpreter.
		switch fieldType {
		case "text":
			current = ""
		case "number":
			current = float64(0)
		case "flag":
			current = false
		default:
			return false
		}
	}
	switch fieldType {
	case "text":
		text, ok := current.(string)
		if !ok {
			text = ""
		}
		targetText, ok := target.(string)
		if !ok {
			return false
		}
		switch condition.Op {
		case "eq":
			return text == targetText
		case "ne":
			return text != targetText
		}
	case "number":
		number, ok := current.(float64)
		if !ok {
			return false
		}
		targetNumber, ok := target.(float64)
		if !ok {
			return false
		}
		switch condition.Op {
		case "eq":
			return number == targetNumber
		case "ne":
			return number != targetNumber
		case "gt":
			return number > targetNumber
		case "gte":
			return number >= targetNumber
		case "lt":
			return number < targetNumber
		case "lte":
			return number <= targetNumber
		}
	case "flag":
		flag, ok := current.(bool)
		if !ok {
			return false
		}
		switch condition.Op {
		case "truthy":
			return flag
		case "falsy":
			return !flag
		case "eq", "ne":
			targetFlag, ok := target.(bool)
			if !ok {
				return false
			}
			if condition.Op == "eq" {
				return flag == targetFlag
			}
			return flag != targetFlag
		}
	}
	return false
}

// TemplateEntity is the bounded data an interpolation can read.
type TemplateEntity struct {
	ID     string
	Name   string
	Text   string
	Status string
	Fields map[string]any
}

// TemplateContext supplies the world and current item values.
type TemplateContext struct {
	Title    string
	Premise  string
	Entities map[string]TemplateEntity
	Item     *TemplateEntity
}

var templatePattern = regexp.MustCompile(`\{\{([^{}]{1,200})\}\}`)

// ValidateTemplate checks every bounded state reference in a text block.
func ValidateTemplate(text string, ctx ValidateContext) error {
	if runeLength(text) > maxTemplateRunes {
		return fmt.Errorf("%w: template text", ErrInvalid)
	}
	matches := templatePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) > 16 {
		return fmt.Errorf("%w: template references", ErrInvalid)
	}
	stripped := templatePattern.ReplaceAllString(text, "")
	if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
		return fmt.Errorf("%w: template syntax", ErrInvalid)
	}
	for _, match := range matches {
		token := strings.TrimSpace(text[match[2]:match[3]])
		if err := validateTemplateToken(token, ctx); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplateToken(token string, ctx ValidateContext) error {
	if token == "world.title" || token == "world.premise" {
		return nil
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return fmt.Errorf("%w: template token %q", ErrInvalid, token)
	}
	if parts[0] == "item" {
		if ctx.ItemKindID == "" {
			return fmt.Errorf("%w: item template outside collection", ErrInvalid)
		}
		kind := ctx.World.Kinds[ctx.ItemKindID]
		return validateEntityTemplateParts(parts[1:], kind)
	}
	if parts[0] == "entity" {
		if len(parts) < 3 {
			return fmt.Errorf("%w: template token %q", ErrInvalid, token)
		}
		entity, ok := ctx.World.Entities[parts[1]]
		if !ok {
			return fmt.Errorf("%w: template entity", ErrInvalid)
		}
		kind := ctx.World.Kinds[entity.KindID]
		return validateEntityTemplateParts(parts[2:], kind)
	}
	return fmt.Errorf("%w: template token %q", ErrInvalid, token)
}

func validateEntityTemplateParts(parts []string, kind KindContext) error {
	if len(parts) == 1 {
		switch parts[0] {
		case "name", "text", "status":
			return nil
		default:
			return fmt.Errorf("%w: template property", ErrInvalid)
		}
	}
	if len(parts) == 2 && parts[0] == "field" {
		if _, ok := kind.Fields[parts[1]]; !ok {
			return fmt.Errorf("%w: template field", ErrInvalid)
		}
		return nil
	}
	return fmt.Errorf("%w: template property", ErrInvalid)
}

// RenderTemplate replaces validated state references with plain text. It is
// intentionally not an expression language: unknown references render empty.
func RenderTemplate(text string, data TemplateContext) string {
	return templatePattern.ReplaceAllStringFunc(text, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		return renderTemplateToken(inner, data)
	})
}

func renderTemplateToken(token string, data TemplateContext) string {
	if token == "world.title" {
		return data.Title
	}
	if token == "world.premise" {
		return data.Premise
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	var entity *TemplateEntity
	var rest []string
	switch parts[0] {
	case "item":
		entity = data.Item
		rest = parts[1:]
	case "entity":
		if len(parts) < 3 {
			return ""
		}
		value, ok := data.Entities[parts[1]]
		if ok {
			entity = &value
		}
		rest = parts[2:]
	default:
		return ""
	}
	if entity == nil {
		return ""
	}
	return templateEntityValue(entity, rest)
}

func templateEntityValue(entity *TemplateEntity, parts []string) string {
	if len(parts) == 1 {
		switch parts[0] {
		case "name":
			return entity.Name
		case "text":
			return entity.Text
		case "status":
			return entity.Status
		}
		return ""
	}
	if len(parts) == 2 && parts[0] == "field" {
		return formatTemplateValue(entity.Fields[parts[1]])
	}
	return ""
}

func formatTemplateValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return fmt.Sprint(typed)
	}
}

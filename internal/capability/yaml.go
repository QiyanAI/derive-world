// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// This file implements the closed YAML subset used by capabilities.yaml.
// Anchors, aliases, tags, multi-document streams, block scalars, and merge
// keys are rejected. Startup fails on any construct the parser does not
// understand; it never guesses.

// ErrConfigInvalid reports a malformed capabilities configuration.
var ErrConfigInvalid = errors.New("invalid capabilities configuration")

type yamlLine struct {
	indent int
	text   string
	number int
}

func parseYAML(data []byte) (any, error) {
	raw := strings.ReplaceAll(string(data), "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := make([]yamlLine, 0)
	for index, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, "\t") {
			return nil, fmt.Errorf("%w: line %d contains a tab", ErrConfigInvalid, index+1)
		}
		trimmed := strings.TrimRight(line, " ")
		stripped := stripYAMLComment(trimmed)
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		indent := len(stripped) - len(strings.TrimLeft(stripped, " "))
		if indent%2 != 0 {
			return nil, fmt.Errorf("%w: line %d indentation must be a multiple of two spaces", ErrConfigInvalid, index+1)
		}
		lines = append(lines, yamlLine{indent: indent, text: strings.TrimSpace(stripped), number: index + 1})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: document is empty", ErrConfigInvalid)
	}
	if lines[0].indent != 0 {
		return nil, fmt.Errorf("%w: line %d must start at indentation zero", ErrConfigInvalid, lines[0].number)
	}
	value, next, err := parseYAMLBlock(lines, 0, 0)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("%w: line %d has unexpected indentation", ErrConfigInvalid, lines[next].number)
	}
	return value, nil
}

func stripYAMLComment(line string) string {
	quote := byte(0)
	for index := 0; index < len(line); index++ {
		char := line[index]
		if quote != 0 {
			if char == quote {
				if quote == '\'' && index+1 < len(line) && line[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			} else if char == '\\' && quote == '"' {
				index++
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '#':
			if index == 0 || line[index-1] == ' ' {
				return strings.TrimRight(line[:index], " ")
			}
		}
	}
	return line
}

func parseYAMLBlock(lines []yamlLine, index, indent int) (any, int, error) {
	if index >= len(lines) {
		return nil, index, fmt.Errorf("%w: unexpected end of document", ErrConfigInvalid)
	}
	if lines[index].indent != indent {
		return nil, index, fmt.Errorf("%w: line %d indentation", ErrConfigInvalid, lines[index].number)
	}
	if isYAMLSequenceLine(lines[index].text) {
		return parseYAMLSequence(lines, index, indent)
	}
	return parseYAMLMapping(lines, index, indent)
}

func isYAMLSequenceLine(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func parseYAMLMapping(lines []yamlLine, index, indent int) (map[string]any, int, error) {
	result := map[string]any{}
	for index < len(lines) && lines[index].indent == indent {
		line := lines[index]
		if isYAMLSequenceLine(line.text) {
			return nil, index, fmt.Errorf("%w: line %d mixes a sequence into a mapping", ErrConfigInvalid, line.number)
		}
		key, rest, ok := splitYAMLKey(line.text)
		if !ok {
			return nil, index, fmt.Errorf("%w: line %d is not a mapping entry", ErrConfigInvalid, line.number)
		}
		if _, exists := result[key]; exists {
			return nil, index, fmt.Errorf("%w: line %d repeats key %q", ErrConfigInvalid, line.number, key)
		}
		index++
		if rest == "" {
			if index < len(lines) && lines[index].indent > indent {
				child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
				if err != nil {
					return nil, index, err
				}
				result[key] = child
				index = next
			} else {
				result[key] = nil
			}
			continue
		}
		value, err := parseYAMLValue(rest, line.number)
		if err != nil {
			return nil, index, err
		}
		result[key] = value
		if index < len(lines) && lines[index].indent > indent {
			return nil, index, fmt.Errorf("%w: line %d has unexpected indentation after a scalar", ErrConfigInvalid, lines[index].number)
		}
	}
	return result, index, nil
}

func parseYAMLSequence(lines []yamlLine, index, indent int) ([]any, int, error) {
	result := make([]any, 0)
	for index < len(lines) && lines[index].indent == indent && isYAMLSequenceLine(lines[index].text) {
		line := lines[index]
		rest := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		index++
		if rest == "" {
			if index >= len(lines) || lines[index].indent <= indent {
				result = append(result, nil)
				continue
			}
			child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
			if err != nil {
				return nil, index, err
			}
			result = append(result, child)
			index = next
			continue
		}
		// "- key: value" starts a mapping whose effective indentation is the
		// column after the dash.
		if key, valueText, ok := splitYAMLKey(rest); ok {
			item, next, err := parseYAMLSequenceMapping(lines, index, indent+2, key, valueText, line.number)
			if err != nil {
				return nil, index, err
			}
			result = append(result, item)
			index = next
			continue
		}
		value, err := parseYAMLValue(rest, line.number)
		if err != nil {
			return nil, index, err
		}
		result = append(result, value)
		if index < len(lines) && lines[index].indent > indent {
			return nil, index, fmt.Errorf("%w: line %d has unexpected indentation after a scalar", ErrConfigInvalid, lines[index].number)
		}
	}
	if index < len(lines) && lines[index].indent > indent {
		return nil, index, fmt.Errorf("%w: line %d has unexpected indentation", ErrConfigInvalid, lines[index].number)
	}
	return result, index, nil
}

func parseYAMLSequenceMapping(lines []yamlLine, index, indent int, firstKey, firstValue string, firstLine int) (map[string]any, int, error) {
	result := map[string]any{}
	key := firstKey
	rest := firstValue
	lineNumber := firstLine
	for {
		if _, exists := result[key]; exists {
			return nil, index, fmt.Errorf("%w: line %d repeats key %q", ErrConfigInvalid, lineNumber, key)
		}
		if rest == "" {
			if index < len(lines) && lines[index].indent > indent {
				child, next, err := parseYAMLBlock(lines, index, lines[index].indent)
				if err != nil {
					return nil, index, err
				}
				result[key] = child
				index = next
			} else {
				result[key] = nil
			}
		} else {
			value, err := parseYAMLValue(rest, lineNumber)
			if err != nil {
				return nil, index, err
			}
			result[key] = value
		}
		if index >= len(lines) || lines[index].indent != indent || isYAMLSequenceLine(lines[index].text) {
			break
		}
		line := lines[index]
		nextKey, nextValue, ok := splitYAMLKey(line.text)
		if !ok {
			return nil, index, fmt.Errorf("%w: line %d is not a mapping entry", ErrConfigInvalid, line.number)
		}
		key, rest, lineNumber = nextKey, nextValue, line.number
		index++
	}
	if index < len(lines) && lines[index].indent > indent {
		return nil, index, fmt.Errorf("%w: line %d has unexpected indentation", ErrConfigInvalid, lines[index].number)
	}
	return result, index, nil
}

// splitYAMLKey splits at the first colon that is outside quotes and flow
// brackets and is followed by whitespace or the end of the line.
func splitYAMLKey(text string) (string, string, bool) {
	quote := byte(0)
	depth := 0
	for index := 0; index < len(text); index++ {
		char := text[index]
		if quote != 0 {
			if char == quote {
				if quote == '\'' && index+1 < len(text) && text[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			} else if char == '\\' && quote == '"' {
				index++
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ':':
			if depth != 0 {
				continue
			}
			if index+1 < len(text) && text[index+1] != ' ' {
				continue
			}
			keyText := strings.TrimSpace(text[:index])
			valueText := strings.TrimSpace(text[index+1:])
			if keyText == "" {
				return "", "", false
			}
			key, err := parseYAMLKey(keyText)
			if err != nil {
				return "", "", false
			}
			return key, valueText, true
		}
	}
	return "", "", false
}

func parseYAMLKey(text string) (string, error) {
	if len(text) >= 2 && ((text[0] == '"' && text[len(text)-1] == '"') || (text[0] == '\'' && text[len(text)-1] == '\'')) {
		value, err := parseYAMLQuoted(text)
		if err != nil {
			return "", err
		}
		return value, nil
	}
	if strings.ContainsAny(text, "{}[],#&*!|>@`") {
		return "", fmt.Errorf("%w: unsupported key %q", ErrConfigInvalid, text)
	}
	return text, nil
}

func parseYAMLValue(text string, lineNumber int) (any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	switch text[0] {
	case '{':
		return parseYAMLFlowMapping(text, lineNumber)
	case '[':
		return parseYAMLFlowSequence(text, lineNumber)
	case '&', '*', '!', '|', '>', '@', '`':
		return nil, fmt.Errorf("%w: line %d uses an unsupported YAML construct", ErrConfigInvalid, lineNumber)
	case '"', '\'':
		return parseYAMLQuoted(text)
	}
	return parseYAMLScalar(text), nil
}

func parseYAMLQuoted(text string) (string, error) {
	if len(text) < 2 {
		return "", fmt.Errorf("%w: unterminated quoted scalar", ErrConfigInvalid)
	}
	switch text[0] {
	case '"':
		if text[len(text)-1] != '"' {
			return "", fmt.Errorf("%w: unterminated double-quoted scalar", ErrConfigInvalid)
		}
		value, err := strconv.Unquote(text)
		if err != nil {
			return "", fmt.Errorf("%w: invalid double-quoted scalar", ErrConfigInvalid)
		}
		return value, nil
	case '\'':
		if text[len(text)-1] != '\'' {
			return "", fmt.Errorf("%w: unterminated single-quoted scalar", ErrConfigInvalid)
		}
		return strings.ReplaceAll(text[1:len(text)-1], "''", "'"), nil
	default:
		return "", fmt.Errorf("%w: not a quoted scalar", ErrConfigInvalid)
	}
}

func parseYAMLScalar(text string) any {
	switch text {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}
	if integer, err := strconv.ParseInt(text, 10, 64); err == nil {
		return integer
	}
	if float, err := strconv.ParseFloat(text, 64); err == nil && strings.ContainsAny(text, ".eE") {
		return float
	}
	return text
}

func parseYAMLFlowMapping(text string, lineNumber int) (map[string]any, error) {
	if !strings.HasPrefix(text, "{") || !strings.HasSuffix(text, "}") {
		return nil, fmt.Errorf("%w: line %d has an unterminated flow mapping", ErrConfigInvalid, lineNumber)
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	result := map[string]any{}
	if body == "" {
		return result, nil
	}
	parts, err := splitYAMLFlow(body)
	if err != nil {
		return nil, fmt.Errorf("%w: line %d: %v", ErrConfigInvalid, lineNumber, err)
	}
	for _, part := range parts {
		key, valueText, ok := splitYAMLKey(part)
		if !ok {
			return nil, fmt.Errorf("%w: line %d flow mapping entry is invalid", ErrConfigInvalid, lineNumber)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("%w: line %d repeats key %q", ErrConfigInvalid, lineNumber, key)
		}
		value, err := parseYAMLValue(valueText, lineNumber)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func parseYAMLFlowSequence(text string, lineNumber int) ([]any, error) {
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil, fmt.Errorf("%w: line %d has an unterminated flow sequence", ErrConfigInvalid, lineNumber)
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	result := make([]any, 0)
	if body == "" {
		return result, nil
	}
	parts, err := splitYAMLFlow(body)
	if err != nil {
		return nil, fmt.Errorf("%w: line %d: %v", ErrConfigInvalid, lineNumber, err)
	}
	for _, part := range parts {
		value, err := parseYAMLValue(part, lineNumber)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func splitYAMLFlow(text string) ([]string, error) {
	parts := make([]string, 0)
	quote := byte(0)
	depth := 0
	start := 0
	for index := 0; index < len(text); index++ {
		char := text[index]
		if quote != 0 {
			if char == quote {
				if quote == '\'' && index+1 < len(text) && text[index+1] == '\'' {
					index++
					continue
				}
				quote = 0
			} else if char == '\\' && quote == '"' {
				index++
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced flow collection")
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(text[start:index])
				if part == "" {
					return nil, errors.New("empty flow collection entry")
				}
				parts = append(parts, part)
				start = index + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, errors.New("unbalanced flow collection")
	}
	part := strings.TrimSpace(text[start:])
	if part != "" {
		parts = append(parts, part)
	}
	return parts, nil
}

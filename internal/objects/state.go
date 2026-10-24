// SPDX-License-Identifier: Apache-2.0
package objects

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/store"
)

const (
	maxStateBytes         = 64 * 1024
	maxSharedContextBytes = 8 * 1024
	maxTitleRunes         = 200
	maxSummaryRunes       = 2000
	maxBodyRunes          = 20000
	maxChecklistItems     = 100
	maxChecklistTextRunes = 500
	maxTags               = 20
	maxTagRunes           = 50
	maxPromptRunes        = 2000
	maxOptionLabelRunes   = 200
	minOptions            = 2
	maxOptions            = 10
)

func decodeStrict(raw []byte, dst any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("value is required")
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return errors.New("null is not allowed")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("value must contain a single JSON document")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func runeLength(value string) int {
	return utf8.RuneCountInString(value)
}

// ValidateCreate validates and normalizes a create request. It returns the
// canonical state and shared context bytes written to the database.
func ValidateCreate(input CreateInput) (state json.RawMessage, shared json.RawMessage, err error) {
	if input.ObjectType != TypeCollaborative && input.ObjectType != TypeInteractive {
		return nil, nil, fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
	title := strings.TrimSpace(input.Title)
	if runeLength(title) < 1 || runeLength(title) > maxTitleRunes {
		return nil, nil, fmt.Errorf("%w: title", ErrInvalidRequest)
	}
	shared, err = normalizeSharedContext(input.SharedContext)
	if err != nil {
		return nil, nil, err
	}

	switch input.ObjectType {
	case TypeCollaborative:
		var content CollaborativeContent
		if len(bytes.TrimSpace(input.Content)) > 0 {
			if err := decodeStrict(input.Content, &content); err != nil {
				return nil, nil, fmt.Errorf("%w: content", ErrInvalidRequest)
			}
		}
		stateValue := CollaborativeState{
			Title:     title,
			Summary:   content.Summary,
			Body:      content.Body,
			Checklist: make([]ChecklistItem, 0, len(content.Checklist)),
			Tags:      normalizeTags(content.Tags),
		}
		for _, item := range content.Checklist {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, nil, err
			}
			stateValue.Checklist = append(stateValue.Checklist, ChecklistItem{
				ID: id.String(), Text: item.Text, Done: item.Done,
			})
		}
		if err := ValidateCollaborativeState(&stateValue); err != nil {
			return nil, nil, err
		}
		state, err = marshalState(stateValue)
		return state, shared, err
	case TypeInteractive:
		var content InteractiveContent
		if len(bytes.TrimSpace(input.Content)) == 0 {
			return nil, nil, fmt.Errorf("%w: content.options", ErrInvalidRequest)
		}
		if err := decodeStrict(input.Content, &content); err != nil {
			return nil, nil, fmt.Errorf("%w: content", ErrInvalidRequest)
		}
		stateValue := InteractiveState{
			Title:   title,
			Prompt:  content.Prompt,
			Options: make([]InteractiveOption, 0, len(content.Options)),
			Votes:   map[string]int{},
			Status:  "open",
		}
		for _, option := range content.Options {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, nil, err
			}
			optionID := id.String()
			stateValue.Options = append(stateValue.Options, InteractiveOption{
				ID: optionID, Label: option.Label,
			})
			stateValue.Votes[optionID] = 0
		}
		if err := ValidateInteractiveState(&stateValue); err != nil {
			return nil, nil, err
		}
		state, err = marshalState(stateValue)
		return state, shared, err
	default:
		return nil, nil, fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
}

func normalizeSharedContext(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value map[string]any
	if err := decodeStrict(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("%w: shared_context", ErrInvalidRequest)
	}
	encoded, err := store.MarshalCompactJSON(value)
	if err != nil {
		return nil, fmt.Errorf("%w: shared_context", ErrInvalidRequest)
	}
	// D04-21 defines the byte cap on PostgreSQL jsonb text. CompactJSONSize
	// measures that representation without Go's HTML escaping, so a legal
	// context containing &, < or > is not rejected by an encoding artifact.
	size, err := store.CompactJSONSize(value)
	if err != nil {
		return nil, fmt.Errorf("%w: shared_context", ErrInvalidRequest)
	}
	if size > maxSharedContextBytes {
		return nil, fmt.Errorf("%w: shared_context", ErrPayloadTooLarge)
	}
	return encoded, nil
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func marshalState(value any) (json.RawMessage, error) {
	encoded, err := store.MarshalCompactJSON(value)
	if err != nil {
		return nil, err
	}
	// Measure the jsonb text form, not Go's HTML-escaped encoding. The
	// database CHECK remains the final authority for the exact text length.
	size, err := store.CompactJSONSize(value)
	if err != nil {
		return nil, err
	}
	if size > maxStateBytes {
		return nil, fmt.Errorf("%w: state", ErrPayloadTooLarge)
	}
	return encoded, nil
}

// ValidateStateBytes validates persisted or assembled state against the
// template schema and the 64 KiB cap.
func ValidateStateBytes(objectType string, raw []byte) error {
	if len(raw) > maxStateBytes {
		return fmt.Errorf("%w: state", ErrPayloadTooLarge)
	}
	switch objectType {
	case TypeCollaborative:
		var state CollaborativeState
		if err := decodeStrict(raw, &state); err != nil {
			return fmt.Errorf("%w: state", ErrInvalidRequest)
		}
		return ValidateCollaborativeState(&state)
	case TypeInteractive:
		var state InteractiveState
		if err := decodeStrict(raw, &state); err != nil {
			return fmt.Errorf("%w: state", ErrInvalidRequest)
		}
		return ValidateInteractiveState(&state)
	default:
		return fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
}

// ValidateCollaborativeState checks the frozen collaborative_v1 schema and
// cross-field uniqueness rules.
func ValidateCollaborativeState(state *CollaborativeState) error {
	if runeLength(strings.TrimSpace(state.Title)) < 1 || runeLength(state.Title) > maxTitleRunes {
		return fmt.Errorf("%w: /title", ErrInvalidRequest)
	}
	if runeLength(state.Summary) > maxSummaryRunes {
		return fmt.Errorf("%w: /summary", ErrInvalidRequest)
	}
	if runeLength(state.Body) > maxBodyRunes {
		return fmt.Errorf("%w: /body", ErrInvalidRequest)
	}
	if len(state.Checklist) > maxChecklistItems {
		return fmt.Errorf("%w: /checklist", ErrInvalidRequest)
	}
	seenItems := make(map[string]bool, len(state.Checklist))
	for index, item := range state.Checklist {
		parsed, err := uuid.Parse(item.ID)
		if err != nil || parsed.Version() != 7 {
			return fmt.Errorf("%w: /checklist/%d/id", ErrInvalidRequest, index)
		}
		if seenItems[item.ID] {
			return fmt.Errorf("%w: /checklist/%d/id", ErrInvalidRequest, index)
		}
		seenItems[item.ID] = true
		if runeLength(strings.TrimSpace(item.Text)) < 1 || runeLength(item.Text) > maxChecklistTextRunes {
			return fmt.Errorf("%w: /checklist/%d/text", ErrInvalidRequest, index)
		}
	}
	if len(state.Tags) > maxTags {
		return fmt.Errorf("%w: /tags", ErrInvalidRequest)
	}
	seenTags := make(map[string]bool, len(state.Tags))
	for index, tag := range state.Tags {
		if runeLength(strings.TrimSpace(tag)) < 1 || runeLength(tag) > maxTagRunes {
			return fmt.Errorf("%w: /tags/%d", ErrInvalidRequest, index)
		}
		if seenTags[tag] {
			return fmt.Errorf("%w: /tags/%d", ErrInvalidRequest, index)
		}
		seenTags[tag] = true
	}
	return nil
}

// ValidateInteractiveState checks the frozen interactive_v1 schema.
func ValidateInteractiveState(state *InteractiveState) error {
	if runeLength(strings.TrimSpace(state.Title)) < 1 || runeLength(state.Title) > maxTitleRunes {
		return fmt.Errorf("%w: /title", ErrInvalidRequest)
	}
	if runeLength(state.Prompt) > maxPromptRunes {
		return fmt.Errorf("%w: /prompt", ErrInvalidRequest)
	}
	if len(state.Options) < minOptions || len(state.Options) > maxOptions {
		return fmt.Errorf("%w: /options", ErrInvalidRequest)
	}
	optionIDs := make(map[string]bool, len(state.Options))
	for index, option := range state.Options {
		parsed, err := uuid.Parse(option.ID)
		if err != nil || parsed.Version() != 7 {
			return fmt.Errorf("%w: /options/%d/id", ErrInvalidRequest, index)
		}
		if optionIDs[option.ID] {
			return fmt.Errorf("%w: /options/%d/id", ErrInvalidRequest, index)
		}
		optionIDs[option.ID] = true
		if runeLength(strings.TrimSpace(option.Label)) < 1 || runeLength(option.Label) > maxOptionLabelRunes {
			return fmt.Errorf("%w: /options/%d/label", ErrInvalidRequest, index)
		}
	}
	if len(state.Votes) != len(optionIDs) {
		return fmt.Errorf("%w: /votes", ErrInvalidRequest)
	}
	for optionID, count := range state.Votes {
		if !optionIDs[optionID] {
			return fmt.Errorf("%w: /votes", ErrInvalidRequest)
		}
		if count < 0 {
			return fmt.Errorf("%w: /votes/%s", ErrInvalidRequest, optionID)
		}
	}
	if state.Status != "open" && state.Status != "closed" {
		return fmt.Errorf("%w: /status", ErrInvalidRequest)
	}
	return nil
}

// ActionPlan is a validated, normalized action.
type ActionPlan struct {
	Action     string
	Permission string
	Input      map[string]any
	RawInput   json.RawMessage
}

type setTitleInput struct {
	Title string `json:"title"`
}

type setSummaryInput struct {
	Summary string `json:"summary"`
}

type setBodyInput struct {
	Body string `json:"body"`
}

type addChecklistInput struct {
	Text string `json:"text"`
}

type toggleChecklistInput struct {
	ItemID string `json:"item_id"`
	Done   bool   `json:"done"`
}

type removeChecklistInput struct {
	ItemID string `json:"item_id"`
}

type addTagInput struct {
	Tag string `json:"tag"`
}

type removeTagInput struct {
	Tag string `json:"tag"`
}

type castVoteInput struct {
	OptionID string `json:"option_id"`
}

type emptyInput struct{}

type sharedContextInput struct {
	SharedContext json.RawMessage `json:"shared_context"`
}

// RequiredPermission maps an action name and template to its required object
// permission without validating the action input. The execution chain uses it
// to authorize before schema validation (D04 section 3.7.3).
func RequiredPermission(objectType, action string) (string, bool) {
	switch action {
	case "set_title", "set_summary", "set_body", "add_checklist_item",
		"toggle_checklist_item", "remove_checklist_item", "add_tag", "remove_tag":
		if objectType == TypeCollaborative {
			return PermContribute, true
		}
	case "cast_vote":
		if objectType == TypeInteractive {
			return PermContribute, true
		}
	case "close_poll":
		if objectType == TypeInteractive {
			return PermManage, true
		}
	case "set_shared_context":
		return PermManage, true
	}
	return "", false
}

// validateInputKeys enforces the exact per-action input key set. A missing
// required key is never interpreted as a zero value.
func validateInputKeys(raw json.RawMessage, required, allowed []string) error {
	var keys map[string]json.RawMessage
	if err := decodeStrict(raw, &keys); err != nil {
		return fmt.Errorf("%w: input", ErrInvalidRequest)
	}
	for _, key := range required {
		value, ok := keys[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%w: input.%s", ErrInvalidRequest, key)
		}
	}
	for key := range keys {
		found := false
		for _, allowedKey := range allowed {
			if key == allowedKey {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: input.%s", ErrInvalidRequest, key)
		}
	}
	return nil
}

// PlanAction validates the action naming, template membership, required
// permission, and exact input shape.
func PlanAction(objectType, action string, raw json.RawMessage) (*ActionPlan, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
	}
	plan := &ActionPlan{Action: action, Input: map[string]any{}, RawInput: raw}
	switch action {
	case "set_title":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"title"}, []string{"title"}); err != nil {
			return nil, err
		}
		var input setTitleInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if runeLength(strings.TrimSpace(input.Title)) < 1 || runeLength(input.Title) > maxTitleRunes {
			return nil, fmt.Errorf("%w: input.title", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["title"] = input.Title
	case "set_summary":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"summary"}, []string{"summary"}); err != nil {
			return nil, err
		}
		var input setSummaryInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if runeLength(input.Summary) > maxSummaryRunes {
			return nil, fmt.Errorf("%w: input.summary", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["summary"] = input.Summary
	case "set_body":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"body"}, []string{"body"}); err != nil {
			return nil, err
		}
		var input setBodyInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if runeLength(input.Body) > maxBodyRunes {
			return nil, fmt.Errorf("%w: input.body", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["body"] = input.Body
	case "add_checklist_item":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"text"}, []string{"text"}); err != nil {
			return nil, err
		}
		var input addChecklistInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if runeLength(strings.TrimSpace(input.Text)) < 1 || runeLength(input.Text) > maxChecklistTextRunes {
			return nil, fmt.Errorf("%w: input.text", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["text"] = input.Text
	case "toggle_checklist_item":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"item_id", "done"}, []string{"item_id", "done"}); err != nil {
			return nil, err
		}
		var input toggleChecklistInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if _, err := uuid.Parse(input.ItemID); err != nil {
			return nil, fmt.Errorf("%w: input.item_id", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["item_id"] = input.ItemID
		plan.Input["done"] = input.Done
	case "remove_checklist_item":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"item_id"}, []string{"item_id"}); err != nil {
			return nil, err
		}
		var input removeChecklistInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if _, err := uuid.Parse(input.ItemID); err != nil {
			return nil, fmt.Errorf("%w: input.item_id", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["item_id"] = input.ItemID
	case "add_tag":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"tag"}, []string{"tag"}); err != nil {
			return nil, err
		}
		var input addTagInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		tag := strings.TrimSpace(input.Tag)
		if runeLength(tag) < 1 || runeLength(tag) > maxTagRunes {
			return nil, fmt.Errorf("%w: input.tag", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["tag"] = tag
	case "remove_tag":
		if objectType != TypeCollaborative {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"tag"}, []string{"tag"}); err != nil {
			return nil, err
		}
		var input removeTagInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		tag := strings.TrimSpace(input.Tag)
		if runeLength(tag) < 1 {
			return nil, fmt.Errorf("%w: input.tag", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["tag"] = tag
	case "cast_vote":
		if objectType != TypeInteractive {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, []string{"option_id"}, []string{"option_id"}); err != nil {
			return nil, err
		}
		var input castVoteInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		if _, err := uuid.Parse(input.OptionID); err != nil {
			return nil, fmt.Errorf("%w: input.option_id", ErrInvalidRequest)
		}
		plan.Permission = PermContribute
		plan.Input["option_id"] = input.OptionID
	case "close_poll":
		if objectType != TypeInteractive {
			return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := validateInputKeys(raw, nil, nil); err != nil {
			return nil, err
		}
		var input emptyInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		plan.Permission = PermManage
	case "set_shared_context":
		if err := validateInputKeys(raw, []string{"shared_context"}, []string{"shared_context"}); err != nil {
			return nil, err
		}
		var input sharedContextInput
		if err := decodeStrict(raw, &input); err != nil {
			return nil, fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		shared, err := normalizeSharedContext(input.SharedContext)
		if err != nil {
			return nil, err
		}
		var decoded map[string]any
		if err := json.Unmarshal(shared, &decoded); err != nil {
			return nil, fmt.Errorf("%w: input.shared_context", ErrInvalidRequest)
		}
		plan.Permission = PermManage
		plan.Input["shared_context"] = decoded
	default:
		return nil, fmt.Errorf("%w: action", ErrInvalidRequest)
	}
	return plan, nil
}

// ApplyAction mutates a copy of the state according to the validated plan.
// oldVoteOption is the receipt's previous option for cast_vote, empty when the
// caller has not voted before. It returns the new state, optional new shared
// context, and the vote option recorded in the receipt.
func ApplyStateAction(objectType string, stateRaw, sharedRaw json.RawMessage, plan *ActionPlan, oldVoteOption string) (json.RawMessage, json.RawMessage, string, error) {
	if plan.Action == "set_shared_context" {
		var input sharedContextInput
		if err := decodeStrict(plan.RawInput, &input); err != nil {
			return nil, nil, "", fmt.Errorf("%w: input", ErrInvalidRequest)
		}
		shared, err := normalizeSharedContext(input.SharedContext)
		if err != nil {
			return nil, nil, "", err
		}
		return stateRaw, shared, "", nil
	}

	switch objectType {
	case TypeCollaborative:
		var state CollaborativeState
		if err := decodeStrict(stateRaw, &state); err != nil {
			return nil, nil, "", fmt.Errorf("%w: state", ErrInvalidRequest)
		}
		switch plan.Action {
		case "set_title":
			state.Title = plan.Input["title"].(string)
		case "set_summary":
			state.Summary = plan.Input["summary"].(string)
		case "set_body":
			state.Body = plan.Input["body"].(string)
		case "add_checklist_item":
			if len(state.Checklist) >= maxChecklistItems {
				return nil, nil, "", fmt.Errorf("%w: checklist is full", ErrConflict)
			}
			id, err := uuid.NewV7()
			if err != nil {
				return nil, nil, "", err
			}
			state.Checklist = append(state.Checklist, ChecklistItem{
				ID: id.String(), Text: plan.Input["text"].(string), Done: false,
			})
		case "toggle_checklist_item":
			itemID := plan.Input["item_id"].(string)
			found := false
			for index := range state.Checklist {
				if state.Checklist[index].ID == itemID {
					state.Checklist[index].Done = plan.Input["done"].(bool)
					found = true
					break
				}
			}
			if !found {
				return nil, nil, "", fmt.Errorf("%w: input.item_id", ErrInvalidRequest)
			}
		case "remove_checklist_item":
			itemID := plan.Input["item_id"].(string)
			found := false
			next := make([]ChecklistItem, 0, len(state.Checklist))
			for _, item := range state.Checklist {
				if item.ID == itemID {
					found = true
					continue
				}
				next = append(next, item)
			}
			if !found {
				return nil, nil, "", fmt.Errorf("%w: input.item_id", ErrInvalidRequest)
			}
			state.Checklist = next
		case "add_tag":
			tag := plan.Input["tag"].(string)
			for _, existing := range state.Tags {
				if existing == tag {
					return nil, nil, "", fmt.Errorf("%w: tag already exists", ErrConflict)
				}
			}
			if len(state.Tags) >= maxTags {
				return nil, nil, "", fmt.Errorf("%w: tag limit reached", ErrConflict)
			}
			state.Tags = append(state.Tags, tag)
			sort.Strings(state.Tags)
		case "remove_tag":
			tag := plan.Input["tag"].(string)
			found := false
			next := make([]string, 0, len(state.Tags))
			for _, existing := range state.Tags {
				if existing == tag {
					found = true
					continue
				}
				next = append(next, existing)
			}
			if !found {
				return nil, nil, "", fmt.Errorf("%w: input.tag", ErrInvalidRequest)
			}
			state.Tags = next
		default:
			return nil, nil, "", fmt.Errorf("%w: action", ErrInvalidRequest)
		}
		if err := ValidateCollaborativeState(&state); err != nil {
			return nil, nil, "", err
		}
		encoded, err := marshalState(state)
		return encoded, sharedRaw, "", err
	case TypeInteractive:
		var state InteractiveState
		if err := decodeStrict(stateRaw, &state); err != nil {
			return nil, nil, "", fmt.Errorf("%w: state", ErrInvalidRequest)
		}
		switch plan.Action {
		case "cast_vote":
			if state.Status != "open" {
				return nil, nil, "", fmt.Errorf("%w: poll is closed", ErrConflict)
			}
			optionID := plan.Input["option_id"].(string)
			found := false
			for _, option := range state.Options {
				if option.ID == optionID {
					found = true
					break
				}
			}
			if !found {
				return nil, nil, "", fmt.Errorf("%w: input.option_id", ErrInvalidRequest)
			}
			if oldVoteOption != "" && oldVoteOption != optionID {
				state.Votes[oldVoteOption]--
				if state.Votes[oldVoteOption] < 0 {
					state.Votes[oldVoteOption] = 0
				}
			}
			if oldVoteOption != optionID {
				state.Votes[optionID]++
			}
			if err := ValidateInteractiveState(&state); err != nil {
				return nil, nil, "", err
			}
			encoded, err := marshalState(state)
			return encoded, sharedRaw, optionID, err
		case "close_poll":
			if state.Status == "closed" {
				return nil, nil, "", fmt.Errorf("%w: poll is already closed", ErrConflict)
			}
			state.Status = "closed"
			if err := ValidateInteractiveState(&state); err != nil {
				return nil, nil, "", err
			}
			encoded, err := marshalState(state)
			return encoded, sharedRaw, "", err
		default:
			return nil, nil, "", fmt.Errorf("%w: action", ErrInvalidRequest)
		}
	default:
		return nil, nil, "", fmt.Errorf("%w: object_type", ErrInvalidRequest)
	}
}

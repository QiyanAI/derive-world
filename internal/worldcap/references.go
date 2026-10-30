// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"strings"
)

// ExperienceReferencesEntity reports whether any capability prop, condition or
// action param contains an entity UUID. UUIDs are globally unique, so a plain
// substring check covers explicit ids and {{entity.<id>...}} templates.
func ExperienceReferencesEntity(experience *Experience, entityID string) bool {
	if experience == nil || entityID == "" {
		return false
	}
	return experienceContains(experience, entityID)
}

// ExperienceReferencesKind reports whether any capability prop contains a
// kind UUID (collection/table/space views).
func ExperienceReferencesKind(experience *Experience, kindID string) bool {
	if experience == nil || kindID == "" {
		return false
	}
	return experienceContains(experience, kindID)
}

func experienceContains(experience *Experience, value string) bool {
	encoded, err := json.Marshal(experience)
	if err != nil {
		return true // fail closed: an unmarshalable experience must block removal
	}
	return strings.Contains(string(encoded), value)
}

// ExperienceReferencesEventField reports whether a behavior action or
// condition reads a field through the dynamic $event.entity scope. Static
// validation intentionally allows those fields (the event kind is not known),
// so removing the field must be blocked separately.
func ExperienceReferencesEventField(experience *Experience, field string) bool {
	if experience == nil || field == "" {
		return false
	}
	for index := range experience.Behaviors {
		behavior := &experience.Behaviors[index]
		if conditionReferencesEventField(behavior.Condition, field) {
			return true
		}
		for actionIndex := range behavior.Actions {
			if actionParamsReferenceEventField(behavior.Actions[actionIndex].Params, field) {
				return true
			}
		}
	}
	return false
}

func conditionReferencesEventField(condition *Condition, field string) bool {
	if condition == nil {
		return false
	}
	if condition.EntityID == "$event.entity" && condition.Field == field {
		return true
	}
	if conditionReferencesEventField(condition.Not, field) {
		return true
	}
	for index := range condition.All {
		if conditionReferencesEventField(&condition.All[index], field) {
			return true
		}
	}
	for index := range condition.Any {
		if conditionReferencesEventField(&condition.Any[index], field) {
			return true
		}
	}
	return false
}

func actionParamsReferenceEventField(raw json.RawMessage, field string) bool {
	if len(raw) == 0 {
		return false
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return true // fail closed: malformed stored params must block field removal
	}
	eventScoped := false
	for _, value := range params {
		if text, ok := value.(string); ok && text == "$event.entity" {
			eventScoped = true
			break
		}
	}
	if !eventScoped {
		return false
	}
	target, _ := params["field"].(string)
	return target == field
}

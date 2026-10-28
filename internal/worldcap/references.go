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

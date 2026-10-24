// SPDX-License-Identifier: Apache-2.0
package trust

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// ConfirmationAction values are frozen by G00 contract §A.4.4.
const (
	ConfirmationDeriveCreate     = "derive.create"
	ConfirmationInvitationCreate = "invitation.create"
	ConfirmationCapabilityInvoke = "capability.invoke"
)

const (
	defaultMaxAIActionsPerHour = 20
	maxAIActionsPerHourLimit   = 200
)

// Constraints is the validated delegation constraint shape. Unknown keys are
// rejected; there is no open extension point in v0.1.0.
type Constraints struct {
	RequireConfirmation []string `json:"require_confirmation"`
	MaxAIActionsPerHour int      `json:"max_ai_actions_per_hour"`
}

// DefaultConstraints is frozen by D05-02. It never contains invite or manage.
const DefaultConstraints = `{"require_confirmation":["derive.create","capability.invoke"],"max_ai_actions_per_hour":20}`

// ParseConstraints accepts only the frozen keys and value ranges.
func ParseConstraints(raw []byte) (Constraints, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(DefaultConstraints)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded struct {
		RequireConfirmation []string `json:"require_confirmation"`
		MaxAIActionsPerHour *int     `json:"max_ai_actions_per_hour"`
	}
	if err := decoder.Decode(&decoded); err != nil {
		return Constraints{}, fmt.Errorf("%w: constraints", ErrInvalidRequest)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return Constraints{}, fmt.Errorf("%w: constraints", ErrInvalidRequest)
	} else if !errors.Is(err, io.EOF) {
		return Constraints{}, fmt.Errorf("%w: constraints", ErrInvalidRequest)
	}
	constraints := Constraints{RequireConfirmation: decoded.RequireConfirmation}
	if decoded.MaxAIActionsPerHour == nil {
		constraints.MaxAIActionsPerHour = defaultMaxAIActionsPerHour
	} else {
		constraints.MaxAIActionsPerHour = *decoded.MaxAIActionsPerHour
	}
	if constraints.MaxAIActionsPerHour < 1 || constraints.MaxAIActionsPerHour > maxAIActionsPerHourLimit {
		return Constraints{}, fmt.Errorf("%w: constraints.max_ai_actions_per_hour", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(constraints.RequireConfirmation))
	for _, action := range constraints.RequireConfirmation {
		if !validConfirmationAction(action) || seen[action] {
			return Constraints{}, fmt.Errorf("%w: constraints.require_confirmation", ErrInvalidRequest)
		}
		seen[action] = true
	}
	sort.Strings(constraints.RequireConfirmation)
	return constraints, nil
}

// MarshalCanonical returns the stable storage representation.
func (c Constraints) MarshalCanonical() ([]byte, error) {
	if c.MaxAIActionsPerHour == 0 {
		c.MaxAIActionsPerHour = defaultMaxAIActionsPerHour
	}
	if c.RequireConfirmation == nil {
		c.RequireConfirmation = []string{}
	}
	sort.Strings(c.RequireConfirmation)
	return json.Marshal(c)
}

func (c Constraints) requiresConfirmation(action string) bool {
	for _, value := range c.RequireConfirmation {
		if value == action {
			return true
		}
	}
	return false
}

func validConfirmationAction(action string) bool {
	switch action {
	case ConfirmationDeriveCreate, ConfirmationInvitationCreate, ConfirmationCapabilityInvoke:
		return true
	default:
		return false
	}
}

func mergeConstraints(merged *Constraints, next Constraints) error {
	if next.MaxAIActionsPerHour < 1 {
		return errors.New("invalid delegation constraints")
	}
	if merged.MaxAIActionsPerHour == 0 || next.MaxAIActionsPerHour < merged.MaxAIActionsPerHour {
		merged.MaxAIActionsPerHour = next.MaxAIActionsPerHour
	}
	for _, action := range next.RequireConfirmation {
		found := false
		for _, current := range merged.RequireConfirmation {
			if current == action {
				found = true
				break
			}
		}
		if !found {
			merged.RequireConfirmation = append(merged.RequireConfirmation, action)
		}
	}
	sort.Strings(merged.RequireConfirmation)
	return nil
}

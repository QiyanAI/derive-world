// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"fmt"

	"github.com/QiyanAI/derive-world/internal/objects"
)

// logProposalNotAccepted records why a server-authorized proposal was refused.
// It re-runs the same pure planners the proposal writer uses, so the log names
// the stage (plan, references, or the remaining permission/version/size path)
// without storing model prose. It is diagnostic only and never changes a vote.
func (s *Service) logProposalNotAccepted(input SendMessageInput, action string,
	raw json.RawMessage, path string) {
	if s.logger == nil || input.Object == nil {
		return
	}
	stage := "version_permission_or_size"
	detail := ""
	plan, err := objects.PlanAction(input.Object.ObjectType, action, raw)
	if err != nil {
		stage = "plan"
		detail = truncateActionDetail(err.Error())
		if action == "batch_change" {
			var batch struct {
				Actions []struct {
					Action string          `json:"action"`
					Input  json.RawMessage `json:"input"`
				} `json:"actions"`
			}
			if json.Unmarshal(raw, &batch) == nil {
				for index, sub := range batch.Actions {
					if _, subErr := objects.PlanAction(objects.TypeOpenWorld,
						sub.Action, sub.Input); subErr != nil {
						stage = fmt.Sprintf("plan/actions/%d", index)
						detail = truncateActionDetail(subErr.Error())
						break
					}
				}
			}
		}
	} else if !objects.ReferencesExist(input.Object.ObjectType, input.Object.State, plan) {
		stage = "references"
	}
	s.logger.Error("personal_ai_proposal_not_accepted",
		"action", action, "path", path, "stage", stage, "detail", detail)
}

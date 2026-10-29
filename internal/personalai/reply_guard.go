// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"regexp"
	"strings"
)

// The model is a proposer: it never executes an approval. Two classes of model
// text are therefore always false or misleading and are replaced before the
// reply becomes chat history:
//
//  1. A claim that a world/change/proposal already exists. Only the human
//     approval endpoint executes, and it runs after the model turn. A pending
//     confirmation means nothing has been created yet.
//  2. Guidance that sends the user looking for an approval card somewhere else
//     (the bottom of the page, a sidebar, a separate panel). The confirmation
//     is rendered directly under the reply.
//
// The guard is deliberately narrow: it only runs on a turn that attempted an
// action, carried an explicit create/change intent, or produced an accepted
// action. A question about an earlier world ("did I create this?") is not
// rewritten on the strength of a keyword alone.

var cardGuidancePattern = regexp.MustCompile(`(?i)(提案卡片|提案卡|批准卡片|确认卡片|审批卡片|卡片上的(?:批准|确认)[^。.!?\n]{0,8}按钮|(?:去|请在|找到|寻找|找到底部|页面底部|聊天底部)[^。.!?\n]{0,16}(?:提案|批准|确认|审批)[^。.!?\n]{0,8}(?:卡片|卡)|proposal\s+card|approval\s+card|confirmation\s+card|click[^.\n]{0,32}card)`)

var sentencePattern = regexp.MustCompile(`[^。！？.!?\n]+[。！？.!?]?`)

var clauseSplitPattern = regexp.MustCompile(`——|—|；|;|：|:`)

var claimDonePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bI(?:'ve| have| had)?\s+(?:now\s+)?(?:already\s+)?(?:successfully\s+)?(?:created|approved|applied|executed)\s+(?:the|your|this|a)\s+(?:world|plan|change|proposal|branch)\b`),
	regexp.MustCompile(`(?i)\b(?:the|your|this)\s+(?:world|plan|change|proposal|branch)\s+(?:is|was|has|have|had)\s+(?:(?:now|already|successfully)\s+)*(?:been\s+)?(?:(?:now|already|successfully)\s+)*(?:created|approved|applied|executed|live)\b`),
	regexp.MustCompile(`(?i)\b(?:world|plan|change|proposal|branch)\s+(?:has|have)\s+(?:now\s+|already\s+)?been\s+(?:successfully\s+)?(?:created|approved|applied|executed)\b`),
	regexp.MustCompile(`(?i)\b(?:created|approved|applied|executed)\s+(?:the|your)\s+(?:world|plan|change|proposal|branch)\s+successfully\b`),
	regexp.MustCompile(`(?:世界|方案|改动|提案|分支)(?:已经|已)(?:被)?(?:创建|生效|上线|批准|应用|生成)`),
	regexp.MustCompile(`(?:已经|已)(?:成功)?(?:创建|批准|应用|执行|生成)(?:了)?(?:这个|该|它|这)?(?:世界|方案|改动|提案|分支)`),
	regexp.MustCompile(`(?:已经|已)(?:成功)?(?:为|帮|替)(?:你|您|我)?[^。！？.!?\n]{0,10}(?:创建|建|生成|批准|应用|执行)(?:好|成|了)`),
	regexp.MustCompile(`(?:已经|已)(?:成功)?把[^。！？.!?\n]{0,10}(?:创建|建|生成|批准|应用|执行)(?:好|成|了)`),
	regexp.MustCompile(`(?:已经|已)(?:成功)?(?:创建|建)(?:好|成)了?(?:这个|该|它|这)?(?:世界|方案|改动|提案|分支)`),
	regexp.MustCompile(`(?:已经|已)(?:为你|帮你|替你)(?:成功)?(?:创建|生成|批准|应用)(?:了)?`),
	regexp.MustCompile(`(?:创建|批准|应用|执行|生成)成功了`),
}

// guardAssistantReply returns a truthful, location-accurate reply. It is a
// pure function so it can be unit-tested without a database or provider.
func guardAssistantReply(reply string, zh bool, accepted []AcceptedAction,
	attempted bool, requestedCreation, requestedChange bool) string {
	guarded := strings.TrimSpace(reply)
	if guarded == "" {
		return reply
	}
	hasConfirmation := false
	hasProposal := false
	for _, action := range accepted {
		switch action.Kind {
		case "confirmation":
			hasConfirmation = true
		case "proposal":
			hasProposal = true
		}
	}
	if cardGuidancePattern.MatchString(guarded) {
		guarded = replaceCardGuidance(guarded, zh, hasConfirmation, hasProposal)
	}
	guardClaims := attempted || requestedCreation || requestedChange || hasConfirmation || hasProposal
	if guardClaims && claimsDone(guarded) {
		return doneClaimReply(zh, hasConfirmation, hasProposal)
	}
	return guarded
}

var conditionalClaimMarkers = []string{
	"only after", "after you", "until you", "once you", "when you",
	"not until", "尚未", "还没", "才会", "只有", "之后才",
}

func claimsDone(reply string) bool {
	// RE2 has no lookahead, so a conditional sentence such as "the world is
	// created only after you confirm" is excluded before matching.
	for _, sentence := range sentencePattern.FindAllString(reply, -1) {
		lower := strings.ToLower(sentence)
		conditional := false
		for _, marker := range conditionalClaimMarkers {
			if strings.Contains(lower, marker) {
				conditional = true
				break
			}
		}
		if conditional {
			continue
		}
		for _, pattern := range claimDonePatterns {
			if pattern.MatchString(sentence) {
				return true
			}
		}
	}
	return false
}

// replaceCardGuidance removes the false "go find the card" instruction while
// keeping the rest of the model's prose. If the whole reply was that
// instruction, the replacement sentence stands alone.
func replaceCardGuidance(reply string, zh bool, hasConfirmation, hasProposal bool) string {
	replacement := guidanceReplacement(zh, hasConfirmation, hasProposal)
	lines := strings.Split(reply, "\n")
	for index, line := range lines {
		lines[index] = sentencePattern.ReplaceAllStringFunc(line, func(sentence string) string {
			if !cardGuidancePattern.MatchString(sentence) {
				return sentence
			}
			kept := make([]string, 0, 2)
			for _, part := range clauseSplitPattern.Split(sentence, -1) {
				part = strings.TrimSpace(part)
				if part != "" && !cardGuidancePattern.MatchString(part) {
					kept = append(kept, part)
				}
			}
			if len(kept) == 0 {
				return replacement
			}
			result := strings.Join(kept, "；")
			endsWithStop := false
			for _, ending := range []string{"。", "！", "？", ".", "!", "?"} {
				if strings.HasSuffix(result, ending) {
					endsWithStop = true
					break
				}
			}
			if !endsWithStop {
				result += "。"
			}
			return result
		})
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func guidanceReplacement(zh bool, hasConfirmation, hasProposal bool) string {
	if hasConfirmation {
		if zh {
			return "方案就在这条回复下方；只有你按下确认按钮后才会真正执行。"
		}
		return "The plan is directly under this reply; nothing runs until you press its confirm button. "
	}
	if hasProposal {
		if zh {
			return "方案的预览就在这条回复下方；只有你确认后才会生效。"
		}
		return "The plan is previewed directly under this reply; it takes effect only after you confirm it. "
	}
	if zh {
		return "这次没有生成可确认的方案。你可以让我重新准备一次。"
	}
	return "No confirmable plan was produced this time. You can ask me to prepare one again. "
}

func doneClaimReply(zh bool, hasConfirmation, hasProposal bool) string {
	if hasConfirmation {
		if zh {
			return "我已经准备好一份方案，就在这条回复下方。现在还没有创建任何世界；只有你按下确认按钮后才会创建。"
		}
		return "I have prepared a plan, directly under this reply. Nothing has been created yet; the world is created only after you press its confirm button."
	}
	if hasProposal {
		if zh {
			return "我已经准备好一份方案，就在这条回复下方。这个世界还没有被改动；只有你确认后才会生效。"
		}
		return "I have prepared a plan, directly under this reply. This world has not changed yet; it takes effect only after you confirm."
	}
	if zh {
		return "我还没有创建或批准任何内容。如果你愿意，我可以先准备一份方案。"
	}
	return "I have not created or approved anything. If you want, I can prepare a plan first."
}

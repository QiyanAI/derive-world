// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file owns every user-visible sentence the server authors itself. The
// sentences are derived from closed reason codes, never from provider text, so
// they are safe to persist and render. They exist because "the server rejected
// the action" is not an explanation a normal user can act on.

// containsCJK reports whether the user's own message is mostly written in
// Chinese, which selects the notice language. It never inspects model output.
func containsCJK(value string) bool {
	for _, runeValue := range value {
		if unicode.Is(unicode.Han, runeValue) {
			return true
		}
	}
	return false
}

// looksLikeModelJSON reports whether fallback text is a provider document
// rather than a normal prose answer. Raw JSON must never reach the transcript.
func looksLikeModelJSON(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	if strings.HasPrefix(trimmed, "```") {
		body := strings.TrimPrefix(trimmed, "```")
		if newline := strings.IndexByte(body, '\n'); newline >= 0 {
			body = body[newline+1:]
		}
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
		return strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") ||
			strings.Contains(body, `"proposed_actions"`)
	}
	return strings.Contains(trimmed, `"proposed_actions"`)
}

// fallbackReply returns a short assistant sentence and a stable reason code.
// The raw provider document is never used as the message.
func fallbackReply(zh bool, finishReason string) (string, string) {
	if finishReason == "length" {
		if zh {
			return "这次回答在输出上限处被截断了，我没能把它整理成可执行的方案。", "truncated"
		}
		return "The response was cut off at the output limit, so I could not turn it into an executable plan.", "truncated"
	}
	if zh {
		return "我没能把这次回答整理成可执行的方案。", "unparseable"
	}
	return "I could not turn this response into an executable plan.", "unparseable"
}

func fallbackNotice(zh bool, reason string) string {
	if reason == "truncated" {
		if zh {
			return "没有创建或修改任何东西。请把想法说得更简短，或让我分步完成。"
		}
		return "Nothing was created or changed. Try a shorter request, or let me do it in steps."
	}
	if zh {
		return "没有创建或修改任何东西。请再试一次，或换一种更具体的说法。"
	}
	return "Nothing was created or changed. Try again, or describe the idea a little more concretely."
}

// rejectionExplanation maps one closed rejection stage to a sentence an
// ordinary user can act on. It deliberately never includes field paths,
// provider text, or internal identifiers.
func rejectionExplanation(stage string, zh bool) string {
	switch stage {
	case "title_or_body":
		if zh {
			return "方案缺少标题或内容，所以没有创建。"
		}
		return "The plan had no title or content, so nothing was created."
	case "seed_decode", "reimagine_decode":
		if zh {
			return "方案里的世界结构与当前运行能力不匹配，所以没有创建。换一种更具体的说法，我可以重新准备。"
		}
		return "The plan's structure did not match what the runtime can run, so nothing was created. Rephrase it and I can prepare a new one."
	case "seed_compose", "reimagine_reshape":
		if zh {
			return "方案的细节无法组合成可运行的世界，所以没有创建。我可以按更小的步骤重新准备。"
		}
		return "The plan's details could not be combined into a runnable world, so nothing was created. I can prepare it in smaller steps."
	case "content_encode", "reimagine_encode":
		if zh {
			return "方案无法保存，所以没有创建。请再试一次。"
		}
		return "The plan could not be saved, so nothing was created. Please try again."
	case "capability_id":
		if zh {
			return "这个操作不在我能提议的范围内，所以没有执行。请在界面里直接完成它。"
		}
		return "That action is outside what I can propose, so nothing was executed. Please do it directly in the interface."
	case "permission", "derive_permission":
		if zh {
			return "当前授权不允许这个操作，所以没有执行。"
		}
		return "Your current authorization does not allow that action, so nothing was executed."
	case "input_json", "empty_action":
		if zh {
			return "这条提议的格式无法读取，所以没有执行。请重新提出。"
		}
		return "The proposal could not be read, so nothing was executed. Please ask again."
	case "confirmation_invalid", "proposal_invalid", "derive_invalid":
		if zh {
			return "方案未通过服务器校验，所以没有执行。请换一种说法再试。"
		}
		return "The plan did not pass server validation, so nothing was executed. Try describing it differently."
	case "confirmation_not_accepted", "proposal_not_accepted", "derive_not_accepted":
		if zh {
			return "这个方案现在无法确认，可能已过期或需要更高权限。请重新准备。"
		}
		return "The plan cannot be confirmed now — it may have expired or need higher permission. Please prepare a new one."
	case "action_budget":
		if zh {
			return "本次 AI 操作次数已达上限，请稍后再试。"
		}
		return "The AI action limit for this period has been reached. Please try again later."
	case "context_stale":
		if zh {
			return "世界在你操作期间已经改变，这次提议没有执行。请基于最新内容重新提出。"
		}
		return "The world changed while you were working, so this proposal was not applied. Please ask again against the latest version."
	case "version_mismatch", "object_mismatch", "derive_version":
		if zh {
			return "目标世界或版本已经改变，这次提议没有执行。请重新提出。"
		}
		return "The target world or version changed, so this proposal was not applied. Please ask again."
	case "reimagine_unsupported":
		if zh {
			return "这个世界目前不支持这种 AI 修改方式。请直接使用世界里的编辑入口，或换一种说法。"
		}
		return "This world does not support that kind of AI change yet. Use the in-world editor, or describe the change differently."
	case "derive_forbidden":
		if zh {
			return "当前授权不允许衍生，所以没有创建新世界。"
		}
		return "Your current authorization does not allow derivation, so no new world was created."
	default:
		if zh {
			return "这个操作没有被执行。请再试一次。"
		}
		return "That action was not executed. Please try again."
	}
}

// joinNoticeText deduplicates and bounds the notices attached to one message.
// The database constraint allows 2048 octets; the cap here leaves room for
// multibyte Chinese while staying well below it.
func joinNoticeText(notices []string) string {
	seen := make(map[string]bool, len(notices))
	parts := make([]string, 0, len(notices))
	for _, notice := range notices {
		notice = strings.TrimSpace(notice)
		if notice == "" || seen[notice] {
			continue
		}
		seen[notice] = true
		parts = append(parts, notice)
	}
	joined := strings.Join(parts, " ")
	const maxRunes = 600
	if utf8.RuneCountInString(joined) <= maxRunes {
		return joined
	}
	runes := []rune(joined)
	return string(runes[:maxRunes])
}

// normalizingSeedSurfaces removes only experience layers that cannot be tabs.
// Some providers list "audio", "sound", "video" or "image" beside real
// surfaces; those layers still apply from their own experience block, so
// dropping them from the tab list is a benign, lossless repair. Any other
// unknown value is left in place for the strict validator to reject with a
// precise reason.
func normalizingSeedSurfaces(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return raw, false
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return raw, false
	}
	experienceRaw, ok := document["experience"]
	if !ok {
		return raw, false
	}
	var experience map[string]json.RawMessage
	if err := json.Unmarshal(experienceRaw, &experience); err != nil {
		return raw, false
	}
	surfacesRaw, ok := experience["surfaces"]
	if !ok {
		return raw, false
	}
	var surfaces []json.RawMessage
	if err := json.Unmarshal(surfacesRaw, &surfaces); err != nil {
		return raw, false
	}
	filtered := make([]json.RawMessage, 0, len(surfaces))
	seen := make(map[string]bool, len(surfaces))
	changed := false
	for _, entry := range surfaces {
		var name string
		if err := json.Unmarshal(entry, &name); err != nil {
			filtered = append(filtered, entry)
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(name))
		if !isSurfaceName(lower) && isExperienceLayer(lower) {
			changed = true
			continue
		}
		if seen[lower] {
			changed = true
			continue
		}
		seen[lower] = true
		filtered = append(filtered, entry)
	}
	if !changed {
		return raw, false
	}
	encodedSurfaces, err := json.Marshal(filtered)
	if err != nil {
		return raw, false
	}
	experience["surfaces"] = encodedSurfaces
	encodedExperience, err := json.Marshal(experience)
	if err != nil {
		return raw, false
	}
	document["experience"] = encodedExperience
	encodedDocument, err := json.Marshal(document)
	if err != nil {
		return raw, false
	}
	return encodedDocument, true
}

// isSurfaceName mirrors the worldseed surface vocabulary. It is duplicated
// here deliberately: personalai must not import the compiler, and the two
// lists are locked by tests on both sides.
func isSurfaceName(value string) bool {
	switch value {
	case "live", "space", "story", "workspace", "custom", "scene3d", "media":
		return true
	default:
		return false
	}
}

func isExperienceLayer(value string) bool {
	switch value {
	case "audio", "sound", "music", "video", "image", "photo", "picture", "lighting":
		return true
	default:
		return false
	}
}

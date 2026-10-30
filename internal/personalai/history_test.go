// SPDX-License-Identifier: Apache-2.0
package personalai

import "testing"

func TestFilterUnreadableTurnsDropsOnlyTheServerFallbackPair(t *testing.T) {
	fallbackEN := "Nothing was created or changed. Try again, or describe the idea a little more concretely."
	fallbackZH := "没有创建或修改任何东西。请再试一次，或换一种更具体的说法。"
	structured := "structured"
	fallback := "fallback"
	items := []Message{
		{Role: "user", Content: "first request"},
		{Role: "assistant", Content: "I could not turn this into a plan.", ParseStatus: &fallback, Notice: &fallbackEN},
		{Role: "user", Content: "a plain prose answer"},
		{Role: "assistant", Content: "I can answer that in prose.", ParseStatus: &fallback},
		{Role: "user", Content: "a good turn"},
		{Role: "assistant", Content: "A plan is ready.", ParseStatus: &structured},
		{Role: "user", Content: "second failed request"},
		{Role: "assistant", Content: "没有创建或修改任何东西。", ParseStatus: &fallback, Notice: &fallbackZH},
		{Role: "user", Content: "third request"},
		{Role: "assistant", Content: "Another plan.", ParseStatus: &structured},
	}
	filtered := filterUnreadableTurns(items)
	want := []string{
		"a plain prose answer", "I can answer that in prose.",
		"a good turn", "A plan is ready.",
		"third request", "Another plan.",
	}
	if len(filtered) != len(want) {
		t.Fatalf("filtered %d messages, want %d: %#v", len(filtered), len(want), filtered)
	}
	for index, content := range want {
		if filtered[index].Content != content {
			t.Fatalf("filtered[%d] = %q, want %q", index, filtered[index].Content, content)
		}
	}
}

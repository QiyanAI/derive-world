// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"strings"
	"testing"
)

func TestGuardAssistantReplyReplacesCardGuidance(t *testing.T) {
	accepted := []AcceptedAction{{Kind: "confirmation", Action: "world.create"}}
	cases := []struct {
		name string
		zh   bool
		in   string
		want string
	}{
		{
			name: "english card guidance becomes an inline instruction",
			in:   "I prepared the plan. Click the approve button in the proposal card.",
			want: "directly under this reply",
		},
		{
			name: "chinese card guidance becomes an inline instruction",
			zh:   true,
			in:   "我已经准备好了方案。请点击底部提案卡片上的批准按钮。",
			want: "这条回复下方",
		},
		{
			name: "english bottom-of-page guidance",
			in:   "Scroll to the bottom of the page and find the approval card.",
			want: "directly under this reply",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := guardAssistantReply(test.in, test.zh, accepted, true, false, false)
			if strings.Contains(got, "proposal card") || strings.Contains(got, "提案卡片") {
				t.Fatalf("card guidance survived: %q", got)
			}
			if !strings.Contains(got, test.want) {
				t.Fatalf("guidance was not replaced with %q: %q", test.want, got)
			}
		})
	}
}

func TestGuardAssistantReplyReplacesFalseCreationClaims(t *testing.T) {
	accepted := []AcceptedAction{{Kind: "confirmation", Action: "world.create"}}
	cases := []struct {
		name string
		zh   bool
		in   string
	}{
		{name: "english first person", in: "I have created the world for you."},
		{name: "english passive", in: "Your world has been created successfully."},
		{name: "chinese first person", zh: true, in: "我已经创建了这个世界。"},
		{name: "chinese created-complete", zh: true, in: "我已经创建好了这个世界。"},
		{name: "chinese for-you", zh: true, in: "已经为你创建好了这个世界。"},
		{name: "chinese passive", zh: true, in: "这个世界已经创建成功。"},
		{name: "english has now been", in: "The world has now been created."},
		{name: "chinese for-this-world created-complete", zh: true, in: "我已经为这个世界创建好了。"},
		{name: "chinese take-this-world created-complete", zh: true, in: "我已经把这个世界创建好了。"},
		{name: "chinese success for-you", zh: true, in: "已经成功为你创建好了这个世界。"},
		{name: "chinese success help-you", zh: true, in: "已经成功帮你创建好了这个世界。"},
		{name: "english have now created", in: "I have now created the world."},
		{name: "english already been created", in: "The world has already been created."},
		{name: "english was successfully created", in: "The world was successfully created."},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := guardAssistantReply(test.in, test.zh, accepted, true, true, false)
			if test.zh {
				if strings.Contains(got, "已经创建了这个世界") || strings.Contains(got, "这个世界已经创建成功") {
					t.Fatalf("false claim survived: %q", got)
				}
				if !strings.Contains(got, "只有你按下确认按钮后才会创建") {
					t.Fatalf("missing truthful pending reply: %q", got)
				}
				return
			}
			if strings.Contains(got, "I have created") || strings.Contains(got, "Your world has been created") {
				t.Fatalf("false claim survived: %q", got)
			}
			if !strings.Contains(got, "Nothing has been created yet") {
				t.Fatalf("missing truthful pending reply: %q", got)
			}
		})
	}
}

func TestGuardAssistantReplyLeavesOrdinaryHistoryAlone(t *testing.T) {
	cases := []string{
		"You created this world last week.",
		"创建成功后，世界会出现在你的列表里。",
		"Tell me what you want to create.",
		"I approved of the idea.",
		"The world is created only after you confirm.",
		"我刚才准备了一份方案，你还没有确认。",
	}
	for _, reply := range cases {
		got := guardAssistantReply(reply, false, nil, false, false, false)
		if got != reply {
			t.Fatalf("ordinary reply rewritten: %q -> %q", reply, got)
		}
	}
}

func TestGuardAssistantReplyExplainsNoPlanWhenGuidanceAndNoAction(t *testing.T) {
	got := guardAssistantReply("Click the proposal card to approve.", false, nil, true, true, false)
	if strings.Contains(got, "proposal card") {
		t.Fatalf("card guidance survived: %q", got)
	}
	if !strings.Contains(got, "No confirmable plan") {
		t.Fatalf("missing no-plan explanation: %q", got)
	}
}

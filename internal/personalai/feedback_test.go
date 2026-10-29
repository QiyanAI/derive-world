// SPDX-License-Identifier: Apache-2.0
package personalai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QiyanAI/derive-world/internal/worldseed"
)

func TestNormalizingSeedSurfacesDropsExperienceLayers(t *testing.T) {
	brief := json.RawMessage(`{
		"premise":"A tiny street at dusk.",
		"kinds":[{"name":"Lamp"}],
		"things":[{"kind":0,"name":"One lamp"}],
		"experience":{"surfaces":["scene3d","media","audio","audio"],"primary":"scene3d",
			"scene3d":{"player":0,"views":[{"kind":0,"primitive":"sphere"}]},
			"audio":{"title":"Evening"}}
	}`)
	normalized, changed := normalizingSeedSurfaces(brief)
	if !changed {
		t.Fatal("audio entries were not normalized")
	}
	seed, err := worldseed.Decode(normalized)
	if err != nil {
		t.Fatalf("normalized brief does not decode: %v", err)
	}
	if len(seed.Experience.Surfaces) != 2 ||
		seed.Experience.Surfaces[0] != "scene3d" || seed.Experience.Surfaces[1] != "media" {
		t.Fatalf("surfaces = %#v", seed.Experience.Surfaces)
	}
	if seed.Experience.Audio == nil {
		t.Fatal("the audio layer was dropped from the experience")
	}
}

func TestNormalizingSeedSurfacesKeepsUnknownValues(t *testing.T) {
	brief := json.RawMessage(`{"premise":"x","experience":{"surfaces":["scene3d","bogus"]}}`)
	normalized, changed := normalizingSeedSurfaces(brief)
	if changed {
		t.Fatal("an unknown surface must be left for strict validation")
	}
	if string(normalized) != string(brief) {
		t.Fatal("unchanged brief was rewritten")
	}
}

func TestBuildWorldCreateContentAcceptsAudioLayer(t *testing.T) {
	create := worldCreateActionInput{
		Title: "Dusk Street",
		Brief: json.RawMessage(`{
			"premise":"A tiny street at dusk.",
			"kinds":[{"name":"Lamp"}],
			"things":[{"kind":0,"name":"One lamp"}],
			"experience":{"surfaces":["scene3d","media","audio"],"primary":"scene3d",
				"scene3d":{"player":0,"views":[{"kind":0,"primitive":"sphere"}]},
				"audio":{"title":"Evening","volume":0.3}}
		}`),
	}
	content, rejection := buildWorldCreateContent(create)
	if rejection != nil {
		t.Fatalf("rejected: %s %s", rejection.Stage, rejection.Detail)
	}
	if len(content) == 0 {
		t.Fatal("valid audio-layer brief produced no content")
	}
}

func TestLooksLikeModelJSON(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{`{"reply":"x"}`, true},
		{"```json\n{\"reply\":\"x\"}\n```", true},
		{`{"capability_id":"world.create"}`, true},
		{"I cannot create that world.", false},
		{"Here is how we could approach it: first, choose a place.", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeModelJSON(tc.value); got != tc.want {
			t.Fatalf("looksLikeModelJSON(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestFallbackExplanationIsActionable(t *testing.T) {
	content, reason := fallbackReply(false, "length")
	if reason != "truncated" || !strings.Contains(strings.ToLower(content), "cut off") {
		t.Fatalf("truncated fallback = %q %q", content, reason)
	}
	if notice := fallbackNotice(false, reason); !strings.Contains(notice, "Nothing was created") {
		t.Fatalf("notice = %q", notice)
	}
	content, reason = fallbackReply(true, "")
	if reason != "unparseable" || content == "" {
		t.Fatalf("unparseable fallback = %q %q", content, reason)
	}
	if notice := fallbackNotice(true, reason); !strings.Contains(notice, "没有创建或修改任何东西") {
		t.Fatalf("zh notice = %q", notice)
	}
}

func TestRejectionExplanationsStayUserFacing(t *testing.T) {
	for _, stage := range []string{
		"seed_decode", "seed_compose", "permission", "confirmation_not_accepted",
		"action_budget", "context_stale", "reimagine_decode", "derive_permission",
	} {
		zh := rejectionExplanation(stage, true)
		en := rejectionExplanation(stage, false)
		if zh == "" || en == "" {
			t.Fatalf("stage %q has no explanation", stage)
		}
		if strings.Contains(zh, "/") || strings.Contains(en, "/") {
			t.Fatalf("stage %q leaked a field path: %q / %q", stage, zh, en)
		}
	}
}

func TestJoinNoticeTextDeduplicatesAndBounds(t *testing.T) {
	if got := joinNoticeText([]string{"a", "a", " ", "b"}); got != "a b" {
		t.Fatalf("join = %q", got)
	}
	long := strings.Repeat("错", 1200)
	got := joinNoticeText([]string{long})
	if len([]rune(got)) > 600 {
		t.Fatalf("notice was not bounded: %d runes", len([]rune(got)))
	}
}

func TestNormalizingSeedBriefDropsTextPalette(t *testing.T) {
	brief := json.RawMessage(`{
		"premise":"A rainy city you can walk through.",
		"palette":"rainy dusk, blue and amber",
		"kinds":[{"name":"Street","motion":"none"}],
		"things":[{"kind":0,"name":"Main street","x":0.5,"y":0.5}],
		"experience":{"surfaces":["scene3d"],"primary":"scene3d","scene3d":{"player":0,"views":[{"kind":0,"primitive":"box"}]}}
	}`)
	normalized, changed := normalizingSeedBrief(brief)
	if !changed {
		t.Fatal("text palette was not normalized")
	}
	seed, err := worldseed.Decode(normalized)
	if err != nil {
		t.Fatalf("normalized brief does not decode: %v", err)
	}
	if seed.Palette != nil {
		t.Fatalf("palette = %#v, want nil", seed.Palette)
	}
}

func TestNormalizingSeedBriefCleansPaletteObject(t *testing.T) {
	brief := json.RawMessage(`{
		"premise":"A tiny street at dusk.",
		"palette":{"sky_hue":220,"ground_hue":"140","accent_hue":40,"mood":"dusk"},
		"kinds":[{"name":"Lamp"}],
		"things":[{"kind":0,"name":"One lamp"}]
	}`)
	normalized, changed := normalizingSeedBrief(brief)
	if !changed {
		t.Fatal("invalid palette fields were not normalized")
	}
	seed, err := worldseed.Decode(normalized)
	if err != nil {
		t.Fatalf("normalized brief does not decode: %v", err)
	}
	if seed.Palette == nil || seed.Palette.SkyHue == nil || *seed.Palette.SkyHue != 220 {
		t.Fatalf("sky hue lost: %#v", seed.Palette)
	}
	if seed.Palette.GroundHue != nil || seed.Palette.AccentHue == nil || *seed.Palette.AccentHue != 40 {
		t.Fatalf("palette cleanup = %#v", seed.Palette)
	}
}

func TestNormalizingSeedBriefKeepsValidPalette(t *testing.T) {
	brief := json.RawMessage(`{
		"premise":"A tiny street at dusk.",
		"palette":{"sky_hue":220},
		"kinds":[{"name":"Lamp"}],
		"things":[{"kind":0,"name":"One lamp"}]
	}`)
	normalized, changed := normalizingSeedBrief(brief)
	if changed || string(normalized) != string(brief) {
		t.Fatal("a valid palette must not be rewritten")
	}
}

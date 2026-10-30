// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// mediaCapability displays one uploaded image or video. The bytes are served
// by the asset API under the world's own read permission; the experience only
// stores an opaque asset id. A world variable can drive visibility and video
// playback, so a real world event can start or stop the media.
type mediaCapability struct{}

func (mediaCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.media", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Image or video", Zh: "图像与视频"},
		Description: Localized{En: "Show an uploaded image or a playable video. A world variable can control visibility and playback.", Zh: "显示上传的图像或可播放的视频；世界变量可以控制显示与播放。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"kind": {
					Type: "string", Title: Localized{En: "Media kind", Zh: "媒体类型"},
					Enum: []string{"image", "video"},
				},
				"asset_id": {
					Type: "string", Format: "asset", MaxLength: 36,
					Title:       Localized{En: "File", Zh: "文件"},
					Description: Localized{En: "Upload a file in the Assets panel, then choose it here.", Zh: "先在资源面板上传文件，再在这里选择。"},
				},
				"caption": {
					Type: "string", MaxLength: 300,
					Title: Localized{En: "Caption", Zh: "说明"},
				},
				"fit": {
					Type: "string", Title: Localized{En: "Fit", Zh: "适配"},
					Enum: []string{"contain", "cover"},
				},
				"controls": {
					Type: "boolean", Title: Localized{En: "Playback controls", Zh: "播放控件"},
				},
				"loop": {
					Type: "boolean", Title: Localized{En: "Loop video", Zh: "循环播放"},
				},
				"muted": {
					Type: "boolean", Title: Localized{En: "Muted", Zh: "静音"},
				},
				"autoplay": {
					Type: "boolean", Title: Localized{En: "Autoplay when allowed", Zh: "允许时自动播放"},
				},
				"play_variable": {
					Type: "string", Format: "variable", MaxLength: 40,
					Title:       Localized{En: "Playback variable", Zh: "播放变量"},
					Description: Localized{En: "A flag variable: true plays, false pauses.", Zh: "开关变量：真则播放，假则暂停。"},
				},
				"show_variable": {
					Type: "string", Format: "variable", MaxLength: 40,
					Title:       Localized{En: "Visibility variable", Zh: "显示变量"},
					Description: Localized{En: "Hide the media while this flag is false.", Zh: "该开关为假时隐藏媒体。"},
				},
			},
			Required: []string{"kind"},
		},
	}
}

type mediaProps struct {
	Kind         string `json:"kind"`
	AssetID      string `json:"asset_id"`
	Caption      string `json:"caption"`
	Fit          string `json:"fit"`
	Controls     *bool  `json:"controls"`
	Loop         bool   `json:"loop"`
	Muted        bool   `json:"muted"`
	Autoplay     bool   `json:"autoplay"`
	PlayVariable string `json:"play_variable"`
	ShowVariable string `json:"show_variable"`
}

func (mediaCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props mediaProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	if props.Kind != "image" && props.Kind != "video" {
		return nil, fmt.Errorf("%w: media.kind", ErrInvalid)
	}
	if props.AssetID != "" && !validAssetID(props.AssetID) {
		return nil, fmt.Errorf("%w: media.asset_id", ErrInvalid)
	}
	props.Caption = strings.TrimSpace(props.Caption)
	if runeLength(props.Caption) > 300 {
		return nil, fmt.Errorf("%w: media.caption", ErrInvalid)
	}
	if props.Fit == "" {
		props.Fit = "contain"
	}
	if props.Fit != "contain" && props.Fit != "cover" {
		return nil, fmt.Errorf("%w: media.fit", ErrInvalid)
	}
	if props.Controls == nil {
		controls := props.Kind == "video"
		props.Controls = &controls
	}
	if props.Kind == "image" && (props.Loop || props.Autoplay || props.PlayVariable != "") {
		return nil, fmt.Errorf("%w: media.image", ErrInvalid)
	}
	// Browsers only autoplay muted media. The server rejects a promise the
	// platform cannot keep instead of silently overriding the user's intent.
	if props.Kind == "video" && props.Autoplay && !props.Muted {
		return nil, fmt.Errorf("%w: media.autoplay", ErrInvalid)
	}
	if props.PlayVariable != "" && !validVariableReference(props.PlayVariable, "flag", ctx) {
		return nil, fmt.Errorf("%w: media.play_variable", ErrInvalid)
	}
	if props.ShowVariable != "" && !validVariableReference(props.ShowVariable, "flag", ctx) {
		return nil, fmt.Errorf("%w: media.show_variable", ErrInvalid)
	}
	return json.Marshal(props)
}

func validAssetID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

// validVariableReference accepts an existing variable of the required scalar
// type. An empty type accepts any declared scalar.
func validVariableReference(name, variableType string, ctx ValidateContext) bool {
	if !validVariableName(name) || ctx.World == nil {
		return false
	}
	declared, ok := ctx.World.Variables[name]
	if !ok {
		return false
	}
	return variableType == "" || declared == variableType
}

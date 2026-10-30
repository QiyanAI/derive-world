// SPDX-License-Identifier: Apache-2.0
package worldcap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// audioCapability adds a real audio layer to a world: one ambient loop (an
// uploaded file or a procedural tone) plus bounded cues that fire when a world
// variable changes. It never downloads an external URL. Browser autoplay
// policy is handled by the client; this data only describes intent.
type audioCapability struct{}

func (audioCapability) Descriptor() Descriptor {
	return Descriptor{
		ID: "core.audio", Kind: "presentation", Version: 1,
		Title:       Localized{En: "Sound and music", Zh: "声音与音乐"},
		Description: Localized{En: "An ambient loop plus event cues driven by world variables. Uses uploaded audio or a procedural tone.", Zh: "环境音循环和由世界变量驱动的事件音效；可使用上传音频或程序化音调。"},
		Params: &ParamSchema{
			Type: "object",
			Properties: map[string]*ParamProperty{
				"title": {
					Type: "string", MaxLength: 80,
					Title: Localized{En: "Label", Zh: "名称"},
				},
				"ambient_asset_id": {
					Type: "string", Format: "asset", AssetKind: "audio", MaxLength: 36,
					Title: Localized{En: "Ambient file", Zh: "环境音文件"},
				},
				"tone":   toneSchema(Localized{En: "Procedural ambient tone", Zh: "程序化环境音"}),
				"loop":   {Type: "boolean", Title: Localized{En: "Loop ambient", Zh: "循环环境音"}},
				"volume": {Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(1), Title: Localized{En: "Volume", Zh: "音量"}},
				"muted":  {Type: "boolean", Title: Localized{En: "Start muted", Zh: "初始静音"}},
				"cues": {
					Type: "array", Title: Localized{En: "Event cues", Zh: "事件音效"},
					Items: &ParamProperty{
						Type: "object",
						Properties: map[string]*ParamProperty{
							"variable": {
								Type: "string", Format: "variable", MaxLength: 40,
								Title: Localized{En: "Variable", Zh: "变量"},
							},
							"mode": {
								Type: "string", Title: Localized{En: "When", Zh: "触发时机"},
								Enum: []string{"truthy", "falsy", "change", "increase", "decrease"},
							},
							"asset_id": {
								Type: "string", Format: "asset", AssetKind: "audio", MaxLength: 36,
								Title: Localized{En: "Sound file", Zh: "音效文件"},
							},
							"tone":   toneSchema(Localized{En: "Procedural cue", Zh: "程序化音效"}),
							"volume": {Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(1), Title: Localized{En: "Cue volume", Zh: "音效音量"}},
						},
						Required: []string{"variable", "mode"},
					},
				},
			},
		},
	}
}

func toneSchema(title Localized) *ParamProperty {
	return &ParamProperty{
		Type: "object", Title: title,
		Properties: map[string]*ParamProperty{
			"frequency": {Type: "number", Minimum: floatPointer(40), Maximum: floatPointer(8000), Title: Localized{En: "Frequency", Zh: "频率"}},
			"wave": {
				Type: "string", Title: Localized{En: "Waveform", Zh: "波形"},
				Enum: []string{"sine", "square", "triangle", "sawtooth"},
			},
			"volume": {Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(0.5), Title: Localized{En: "Tone volume", Zh: "音调音量"}},
			"pulse":  {Type: "number", Minimum: floatPointer(0), Maximum: floatPointer(4), Title: Localized{En: "Pulse speed", Zh: "脉动速度"}},
		},
	}
}

type audioTone struct {
	Frequency float64 `json:"frequency"`
	Wave      string  `json:"wave"`
	Volume    float64 `json:"volume"`
	Pulse     float64 `json:"pulse"`
}

type audioCue struct {
	Variable string     `json:"variable"`
	Mode     string     `json:"mode"`
	AssetID  string     `json:"asset_id"`
	Tone     *audioTone `json:"tone,omitempty"`
	Volume   float64    `json:"volume"`
}

type audioProps struct {
	Title          string     `json:"title"`
	AmbientAssetID string     `json:"ambient_asset_id"`
	Tone           *audioTone `json:"tone,omitempty"`
	Loop           *bool      `json:"loop"`
	Volume         float64    `json:"volume"`
	Muted          bool       `json:"muted"`
	Cues           []audioCue `json:"cues,omitempty"`
}

func (audioCapability) Validate(raw json.RawMessage, ctx ValidateContext) (json.RawMessage, error) {
	var props audioProps
	if err := decodeStrict(raw, &props); err != nil {
		return nil, err
	}
	props.Title = strings.TrimSpace(props.Title)
	if runeLength(props.Title) > 80 {
		return nil, fmt.Errorf("%w: audio.title", ErrInvalid)
	}
	if props.AmbientAssetID != "" && !validAssetID(props.AmbientAssetID) {
		return nil, fmt.Errorf("%w: audio.ambient_asset_id", ErrInvalid)
	}
	if props.AmbientAssetID != "" && props.Tone != nil {
		return nil, fmt.Errorf("%w: audio.ambient", ErrInvalid)
	}
	if props.Tone != nil {
		if err := validateTone(props.Tone); err != nil {
			return nil, fmt.Errorf("%w: audio.tone", ErrInvalid)
		}
	}
	if props.Loop == nil {
		loop := true
		props.Loop = &loop
	}
	if props.Volume == 0 {
		props.Volume = 0.5
	}
	if !finite(props.Volume) || props.Volume < 0 || props.Volume > 1 {
		return nil, fmt.Errorf("%w: audio.volume", ErrInvalid)
	}
	if len(props.Cues) > 8 {
		return nil, fmt.Errorf("%w: audio.cues", ErrInvalid)
	}
	for index := range props.Cues {
		cue := &props.Cues[index]
		if !validVariableName(cue.Variable) {
			return nil, fmt.Errorf("%w: audio.cues/%d/variable", ErrInvalid, index)
		}
		declaredType, ok := "", false
		if ctx.World != nil {
			declaredType, ok = ctx.World.Variables[cue.Variable]
		}
		if !ok {
			return nil, fmt.Errorf("%w: audio.cues/%d/variable", ErrInvalid, index)
		}
		switch cue.Mode {
		case "truthy", "falsy":
			if declaredType != "flag" {
				return nil, fmt.Errorf("%w: audio.cues/%d/mode", ErrInvalid, index)
			}
		case "increase", "decrease":
			if declaredType != "number" {
				return nil, fmt.Errorf("%w: audio.cues/%d/mode", ErrInvalid, index)
			}
		case "change":
		default:
			return nil, fmt.Errorf("%w: audio.cues/%d/mode", ErrInvalid, index)
		}
		if cue.AssetID != "" && !validAssetID(cue.AssetID) {
			return nil, fmt.Errorf("%w: audio.cues/%d/asset_id", ErrInvalid, index)
		}
		if (cue.AssetID == "") == (cue.Tone == nil) {
			return nil, fmt.Errorf("%w: audio.cues/%d/source", ErrInvalid, index)
		}
		if cue.Tone != nil {
			if err := validateTone(cue.Tone); err != nil {
				return nil, fmt.Errorf("%w: audio.cues/%d/tone", ErrInvalid, index)
			}
		}
		if cue.Volume == 0 {
			cue.Volume = 0.7
		}
		if !finite(cue.Volume) || cue.Volume < 0 || cue.Volume > 1 {
			return nil, fmt.Errorf("%w: audio.cues/%d/volume", ErrInvalid, index)
		}
	}
	return json.Marshal(props)
}

func validateTone(tone *audioTone) error {
	if tone == nil {
		return fmt.Errorf("%w: tone", ErrInvalid)
	}
	if !finite(tone.Frequency) || tone.Frequency < 40 || tone.Frequency > 8000 {
		return fmt.Errorf("%w: audio.tone.frequency", ErrInvalid)
	}
	if tone.Wave == "" {
		tone.Wave = "sine"
	}
	if tone.Wave != "sine" && tone.Wave != "square" && tone.Wave != "triangle" && tone.Wave != "sawtooth" {
		return fmt.Errorf("%w: audio.tone.wave", ErrInvalid)
	}
	if !finite(tone.Volume) || tone.Volume < 0 || tone.Volume > 0.5 {
		return fmt.Errorf("%w: audio.tone.volume", ErrInvalid)
	}
	if tone.Volume == 0 {
		tone.Volume = 0.08
	}
	if !finite(tone.Pulse) || tone.Pulse < 0 || tone.Pulse > 4 {
		return fmt.Errorf("%w: audio.tone.pulse", ErrInvalid)
	}
	return nil
}

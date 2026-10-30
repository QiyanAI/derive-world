// SPDX-License-Identifier: Apache-2.0
//
// Package worldseed turns a compact, model-authored world intention into a
// real, bounded open_world_v1 document. It is the bridge between free language
// and the capability runtime: the model imagines names, forms, relations and
// interactions; this package lays them out, assigns identity, composes the
// experience surfaces and proves the result validates.
//
// A seed is not a world template. It names no product category and no fixed
// shape vocabulary: forms are arbitrary bounded SVG paths, kinds are whatever
// the author names, and the surface choices (live, story, workspace, space)
// are universal runtimes that can be combined freely. Everything remains
// server-interpreted data; no user code, external asset or network access is
// involved.
package worldseed

import "encoding/json"

// ErrInvalid marks a seed that may not be composed. ErrTooLarge marks a seed
// whose composed world exceeds the creation budget.
var (
	ErrInvalid  = errSeed("invalid world seed")
	ErrTooLarge = errSeed("world seed exceeds the creation budget")
)

type errSeed string

func (e errSeed) Error() string { return string(e) }

// Brief is the model-authored intention. Field names are deliberately short
// and literal so a model can produce one inside the 8 KiB output bound.
type Brief struct {
	Premise      string        `json:"premise"`
	Atmosphere   string        `json:"atmosphere,omitempty"`
	Palette      *Palette      `json:"palette,omitempty"`
	Kinds        []Kind        `json:"kinds"`
	Things       []Thing       `json:"things"`
	Relations    []Relation    `json:"relations,omitempty"`
	Interactions []Interaction `json:"interactions,omitempty"`
	Experience   *Experience   `json:"experience,omitempty"`
}

// Palette chooses the world's ambient hues. All values are optional; the
// composer derives a coherent palette from the seed when they are absent.
type Palette struct {
	SkyHue    *int `json:"sky_hue,omitempty"`
	GroundHue *int `json:"ground_hue,omitempty"`
	AccentHue *int `json:"accent_hue,omitempty"`
}

// Kind is one kind of existence in the world. Form is an arbitrary SVG path in
// a 100-unit box centred on (0,0). Essence is free prose that the composer may
// use when a thing has no description of its own.
type Kind struct {
	Name    string  `json:"name"`
	Essence string  `json:"essence,omitempty"`
	Hue     *int    `json:"hue,omitempty"`
	Motion  string  `json:"motion,omitempty"`
	Form    string  `json:"form,omitempty"`
	Solid   bool    `json:"solid,omitempty"`
	Fields  []Field `json:"fields,omitempty"`
}

// Field is one value slot on a kind. Type is text, number or flag. Initial is
// optional and must match the type.
type Field struct {
	Label   string `json:"label"`
	Type    string `json:"type"`
	Initial any    `json:"initial,omitempty"`
}

// Thing is one concrete existence. Kind is an index into Brief.Kinds. X and Y
// are optional normalised hints in [0,1]; the composer otherwise places things
// itself. Values are keyed by the field label of the thing's kind.
type Thing struct {
	Kind   int            `json:"kind"`
	Name   string         `json:"name"`
	Text   string         `json:"text,omitempty"`
	Values map[string]any `json:"values,omitempty"`
	X      *float64       `json:"x,omitempty"`
	Y      *float64       `json:"y,omitempty"`
}

// Relation is a labelled link between two things.
type Relation struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label,omitempty"`
}

// Interaction is one reachable change. From is the thing the user touches; To
// is the thing that changes (defaults to From). Field is a field label, or
// "status" for open/done. Op is set, add or toggle.
type Interaction struct {
	From  int    `json:"from"`
	To    *int   `json:"to,omitempty"`
	Verb  string `json:"verb"`
	Field string `json:"field,omitempty"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// Experience chooses which universal surfaces carry the world. It is not a
// world category: the same content can be played, watched, read, operated or
// any combination, all sharing one state.
type Experience struct {
	Primary   string         `json:"primary,omitempty"`
	Surfaces  []string       `json:"surfaces,omitempty"`
	Live      *LiveSpec      `json:"live,omitempty"`
	Story     *StorySpec     `json:"story,omitempty"`
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`
	// Scene3D turns the same things into a real three-dimensional scene with
	// camera movement, gravity and collisions. It references no uploaded model
	// in a seed; a human may attach a GLB later in the world editor.
	Scene3D *Scene3DSpec `json:"scene3d,omitempty"`
	// Audio adds an ambient procedural tone and bounded event cues. Uploaded
	// audio files are attached by a human after creation; a seed never invents
	// an asset id.
	Audio *AudioSpec `json:"audio,omitempty"`
	// Media reserves a real image or video slot. The seed cannot carry bytes or
	// asset ids; the human uploads the file in the world editor afterwards.
	Media *MediaSpec `json:"media,omitempty"`
	// Custom is a bounded declarative surface for forms the fixed surfaces do
	// not cover. It is validated by worldcap.ValidateCustom and interpreted by
	// the shared client/server runtime, never compiled to code.
	Custom json.RawMessage `json:"custom,omitempty"`
}

// LiveSpec configures the time-based surface. With no avatar it plays an
// animation; with an avatar it is walkable; with a goal it is a game.
type LiveSpec struct {
	Title     string        `json:"title,omitempty"`
	Hint      string        `json:"hint,omitempty"`
	Avatar    *int          `json:"avatar,omitempty"`
	Speed     *float64      `json:"speed,omitempty"`
	Goal      *GoalSpec     `json:"goal,omitempty"`
	Timer     *float64      `json:"timer,omitempty"`
	Particles *ParticleSpec `json:"particles,omitempty"`
	Timeline  *TimelineSpec `json:"timeline,omitempty"`
}

// GoalSpec is one playable objective. Collect counts done things of a kind;
// score reads a variable or a numeric field; reach tests proximity to a thing;
// survive uses the timer.
type GoalSpec struct {
	Type     string   `json:"type"`
	Label    string   `json:"label,omitempty"`
	Kind     *int     `json:"kind,omitempty"`
	Count    *int     `json:"count,omitempty"`
	Entity   *int     `json:"entity,omitempty"`
	Field    string   `json:"field,omitempty"`
	Variable string   `json:"variable,omitempty"`
	Target   *float64 `json:"target,omitempty"`
}

// ParticleSpec is a bounded ambient animation (rain, motes, stars or snow).
type ParticleSpec struct {
	Kind  string   `json:"kind"`
	Count *int     `json:"count,omitempty"`
	Hue   *int     `json:"hue,omitempty"`
	Size  *float64 `json:"size,omitempty"`
	Speed *float64 `json:"speed,omitempty"`
}

// TimelineSpec is an explicit choreography. When absent the composer derives a
// deterministic motion timeline from each kind's motion.
type TimelineSpec struct {
	Duration *float64    `json:"duration,omitempty"`
	Loop     bool        `json:"loop,omitempty"`
	Autoplay *bool       `json:"autoplay,omitempty"`
	Tracks   []TrackSpec `json:"tracks"`
}

type TrackSpec struct {
	Entity   int       `json:"entity"`
	Property string    `json:"property"`
	Keys     []KeySpec `json:"keys"`
}

type KeySpec struct {
	T      *float64 `json:"t"`
	V      float64  `json:"v"`
	Easing string   `json:"easing,omitempty"`
}

// StorySpec writes chapters as real things of one kind. Choices become real
// links; a shared variable stores the reading position.
type StorySpec struct {
	Title    string        `json:"title,omitempty"`
	Kind     string        `json:"kind,omitempty"`
	Chapters []ChapterSpec `json:"chapters"`
}

type ChapterSpec struct {
	Title   string       `json:"title"`
	Text    string       `json:"text"`
	Choices []ChoiceSpec `json:"choices,omitempty"`
}

type ChoiceSpec struct {
	Label string `json:"label"`
	To    int    `json:"to"`
}

// WorkspaceSpec turns one kind into an operable table. Columns and actions are
// optional; sensible actions are derived from the fields when omitted.
type WorkspaceSpec struct {
	Title   string                `json:"title,omitempty"`
	Kind    *int                  `json:"kind,omitempty"`
	Columns []string              `json:"columns,omitempty"`
	Actions []WorkspaceActionSpec `json:"actions,omitempty"`
}

type WorkspaceActionSpec struct {
	Label string `json:"label"`
	Type  string `json:"type"` // create | done | progress
	Field string `json:"field,omitempty"`
}

// Scene3DSpec places the seed's own things in a 3D scene. Views are keyed by
// kind index; omitted views receive a deterministic primitive. Player is a
// thing index, not a new entity.
type Scene3DSpec struct {
	Title     string            `json:"title,omitempty"`
	SkyHue    *int              `json:"sky_hue,omitempty"`
	GroundHue *int              `json:"ground_hue,omitempty"`
	Fog       *float64          `json:"fog,omitempty"`
	Camera    string            `json:"camera,omitempty"` // orbit | follow
	Distance  *float64          `json:"distance,omitempty"`
	Player    *int              `json:"player,omitempty"`
	Speed     *float64          `json:"speed,omitempty"`
	Gravity   *float64          `json:"gravity,omitempty"`
	Views     []Scene3DViewSpec `json:"views,omitempty"`
}

// Scene3DViewSpec is one kind's 3D form.
type Scene3DViewSpec struct {
	Kind      int      `json:"kind"`
	Primitive string   `json:"primitive,omitempty"`
	Scale     *float64 `json:"scale,omitempty"`
	Elevation *float64 `json:"elevation,omitempty"`
	Collider  string   `json:"collider,omitempty"`
	HideDone  bool     `json:"hide_done,omitempty"`
}

// AudioSpec is a procedural ambient tone plus variable-driven cues. It never
// references an uploaded file; those are attached by the human.
// MediaSpec is an empty media slot. It names the intended kind and the words
// displayed until the human uploads a file.
type MediaSpec struct {
	Title        string `json:"title,omitempty"`
	Kind         string `json:"kind,omitempty"` // image | video
	Caption      string `json:"caption,omitempty"`
	ShowVariable string `json:"show_variable,omitempty"`
	PlayVariable string `json:"play_variable,omitempty"`
}

type AudioSpec struct {
	Title  string    `json:"title,omitempty"`
	Tone   *ToneSpec `json:"tone,omitempty"`
	Loop   *bool     `json:"loop,omitempty"`
	Volume *float64  `json:"volume,omitempty"`
	Cues   []CueSpec `json:"cues,omitempty"`
}

// ToneSpec is a bounded Web Audio oscillator description.
type ToneSpec struct {
	Frequency *float64 `json:"frequency,omitempty"`
	Wave      string   `json:"wave,omitempty"`
	Volume    *float64 `json:"volume,omitempty"`
	Pulse     *float64 `json:"pulse,omitempty"`
}

// CueSpec fires a tone when a world variable changes.
type CueSpec struct {
	Variable string    `json:"variable"`
	Mode     string    `json:"mode,omitempty"` // truthy | falsy | change | increase | decrease
	Tone     *ToneSpec `json:"tone,omitempty"`
	Volume   *float64  `json:"volume,omitempty"`
}

// Decode parses one strict seed document. Unknown fields are rejected so a
// model cannot smuggle presentation or actor overrides through the seed.
func Decode(raw json.RawMessage) (*Brief, error) {
	var brief Brief
	if err := decodeStrict(raw, &brief); err != nil {
		return nil, err
	}
	if err := brief.normalize(); err != nil {
		return nil, err
	}
	return &brief, nil
}

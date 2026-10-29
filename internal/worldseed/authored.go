// SPDX-License-Identifier: Apache-2.0
package worldseed

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// idSource lets the same authoring code mint local refs for a create body and
// real UUIDs for a replacement state. Identity is always server-controlled.
type idSource interface {
	KindID() string
	NodeID() string
	LinkID() string
}

type refSource struct {
	kinds, nodes, links int
}

func (source *refSource) KindID() string {
	id := fmt.Sprintf("k%d", source.kinds)
	source.kinds++
	return id
}

func (source *refSource) NodeID() string {
	id := fmt.Sprintf("n%d", source.nodes)
	source.nodes++
	return id
}

func (source *refSource) LinkID() string {
	id := fmt.Sprintf("l%d", source.links)
	source.links++
	return id
}

type uuidSource struct{}

func (uuidSource) KindID() string { return newID() }
func (uuidSource) NodeID() string { return newID() }
func (uuidSource) LinkID() string { return newID() }

// authoredWorld is the mutable working copy the surface builders share. Its
// ids are refs while composing a create body and UUIDs while reshaping an
// existing world; nothing else differs.
type authoredWorld struct {
	Title   string
	Premise string
	Kinds   []*authoredKind
	Nodes   []*authoredNode
	Links   []*authoredLink
	// Rules, Views and Tags belong to the world, not to any one surface. Reshape
	// must carry them through untouched when a brief changes content or an
	// experience block; dropping them silently would rewrite the user's world.
	Rules        []objects.OpenWorldRule
	Views        []string
	Tags         []string
	Variables    map[string]any
	Presentation objects.OpenWorldPresentation
	ids          idSource
}

type authoredKind struct {
	ID         string
	Name       string
	Essence    string
	Hue        int
	Motion     string
	Form       string
	Solid      bool
	Fields     []objects.OpenWorldField
	Appearance objects.OpenWorldAppearance
	Bindings   []objects.OpenWorldBinding
}

type authoredNode struct {
	ID     string
	KindID string
	Name   string
	Text   string
	Values map[string]any
	Status string
	X      float64
	Y      float64
}

type authoredLink struct {
	ID    string
	From  string
	To    string
	Label string
}

func authoredFromBrief(brief *Brief, title string, ids idSource) (*authoredWorld, error) {
	world := &authoredWorld{
		Title: title, Premise: brief.Premise, Variables: map[string]any{}, ids: ids,
	}
	hash := stableHash(strings.ToLower(strings.TrimSpace(brief.Premise + title)))
	sky, ground, accent := int(hash%360), int((hash+24)%360), int((hash+184)%360)
	if brief.Palette != nil {
		if brief.Palette.SkyHue != nil {
			sky = *brief.Palette.SkyHue
		}
		if brief.Palette.GroundHue != nil {
			ground = *brief.Palette.GroundHue
		}
		if brief.Palette.AccentHue != nil {
			accent = *brief.Palette.AccentHue
		}
	}
	world.Presentation = objects.OpenWorldPresentation{
		Scene: objects.OpenWorldScene{
			Backdrop: objects.OpenWorldBackdropHorizon, SkyHue: sky,
			GroundHue: ground, Horizon: 62, AccentHue: accent,
			Ambient: objects.OpenWorldAmbientStars, AmbientDensity: 30,
		},
		LinkStyle: objects.OpenWorldLinkThread,
	}
	kindBySeed := make([]*authoredKind, len(brief.Kinds))
	for index := range brief.Kinds {
		seed := &brief.Kinds[index]
		hue := (accent + index*47) % 360
		if seed.Hue != nil {
			hue = *seed.Hue
		}
		kind := &authoredKind{
			ID: ids.KindID(), Name: seed.Name, Essence: seed.Essence,
			Hue: hue, Motion: seed.Motion, Form: strings.TrimSpace(seed.Form),
			Solid: seed.Solid, Appearance: objects.DefaultOpenWorldAppearance(),
		}
		if kind.Form == "" {
			kind.Form = generatedForm(seed.Name, index)
		}
		kind.Appearance.Shape = classicShape(index)
		kind.Appearance.Motion = classicMotion(seed.Motion)
		kind.Appearance.Size = 48 + float64(index%3)*10
		for fieldIndex, field := range seed.Fields {
			kind.Fields = append(kind.Fields, objects.OpenWorldField{
				Key: fmt.Sprintf("f%d", fieldIndex), Label: field.Label, Type: field.Type,
			})
		}
		kind.Bindings = bindingsForKind(kind)
		world.Kinds = append(world.Kinds, kind)
		kindBySeed[index] = kind
	}
	for index := range brief.Things {
		seed := &brief.Things[index]
		kind := kindBySeed[seed.Kind]
		node := &authoredNode{
			ID: ids.NodeID(), KindID: kind.ID, Name: seed.Name, Text: seed.Text,
			Values: map[string]any{}, Status: objects.OpenWorldStatusOpen,
		}
		node.X, node.Y = layoutPosition(index, len(brief.Things), seed.Kind, "live")
		if seed.X != nil {
			node.X = 80 + clampFloat(*seed.X, 0, 1)*1440
		}
		if seed.Y != nil {
			node.Y = 90 + clampFloat(*seed.Y, 0, 1)*820
		}
		for fieldIndex, field := range brief.Kinds[seed.Kind].Fields {
			key := fmt.Sprintf("f%d", fieldIndex)
			if value, ok := seed.Values[field.Label]; ok {
				node.Values[key] = value
			} else if field.Initial != nil {
				node.Values[key] = field.Initial
			} else {
				node.Values[key] = zeroValue(field.Type, fieldIndex)
			}
		}
		world.Nodes = append(world.Nodes, node)
	}
	for _, relation := range brief.Relations {
		world.Links = append(world.Links, &authoredLink{
			ID: ids.LinkID(), From: world.Nodes[relation.From].ID,
			To: world.Nodes[relation.To].ID, Label: relation.Label,
		})
	}
	return world, nil
}

func authoredFromState(state *objects.OpenWorldState) *authoredWorld {
	world := &authoredWorld{
		Title: state.Title, Premise: state.Premise,
		Presentation: state.Presentation,
		Rules:        append([]objects.OpenWorldRule(nil), state.Rules...),
		Views:        append([]string(nil), state.Views...),
		Tags:         append([]string(nil), state.Tags...),
		Variables:    map[string]any{}, ids: uuidSource{},
	}
	for name, value := range state.Variables {
		world.Variables[name] = value
	}
	for index := range state.Kinds {
		kind := &state.Kinds[index]
		world.Kinds = append(world.Kinds, &authoredKind{
			ID: kind.ID, Name: kind.Name, Hue: kind.Hue,
			Motion: kind.Appearance.Motion, Form: generatedForm(kind.Name, index),
			Fields:     append([]objects.OpenWorldField(nil), kind.Fields...),
			Appearance: kind.Appearance,
			Bindings:   append([]objects.OpenWorldBinding(nil), kind.Bindings...),
		})
	}
	for index := range state.Nodes {
		node := &state.Nodes[index]
		values := make(map[string]any, len(node.Values))
		for key, value := range node.Values {
			values[key] = value
		}
		world.Nodes = append(world.Nodes, &authoredNode{
			ID: node.ID, KindID: node.KindID, Name: node.Name, Text: node.Text,
			Values: values, Status: node.Status, X: node.X, Y: node.Y,
		})
	}
	for index := range state.Links {
		link := &state.Links[index]
		world.Links = append(world.Links, &authoredLink{
			ID: link.ID, From: link.From, To: link.To, Label: link.Label,
		})
	}
	return world
}

func (world *authoredWorld) findKindByName(name string) *authoredKind {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, kind := range world.Kinds {
		if strings.ToLower(strings.TrimSpace(kind.Name)) == lower {
			return kind
		}
	}
	return nil
}

func (world *authoredWorld) findKindByID(id string) *authoredKind {
	for _, kind := range world.Kinds {
		if kind.ID == id {
			return kind
		}
	}
	return nil
}

func (world *authoredWorld) findNodeByName(kindID, name string) *authoredNode {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, node := range world.Nodes {
		if node.KindID == kindID && strings.ToLower(strings.TrimSpace(node.Name)) == lower {
			return node
		}
	}
	return nil
}

func (world *authoredWorld) findNodeByID(id string) *authoredNode {
	for _, node := range world.Nodes {
		if node.ID == id {
			return node
		}
	}
	return nil
}

func (world *authoredWorld) nodesOfKind(kindID string) []*authoredNode {
	nodes := make([]*authoredNode, 0)
	for _, node := range world.Nodes {
		if node.KindID == kindID {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (world *authoredWorld) fieldKey(kindID, label string) string {
	kind := world.findKindByID(kindID)
	if kind == nil {
		return ""
	}
	if label == "" || label == "status" {
		return "status"
	}
	for _, field := range kind.Fields {
		if field.Label == label {
			return field.Key
		}
	}
	return ""
}

func (world *authoredWorld) fieldType(kindID, key string) string {
	if key == "status" {
		return "status"
	}
	kind := world.findKindByID(kindID)
	if kind == nil {
		return ""
	}
	for _, field := range kind.Fields {
		if field.Key == key {
			return field.Type
		}
	}
	return ""
}

func (world *authoredWorld) addKind(name string, hue int, form string) *authoredKind {
	kind := &authoredKind{
		ID: world.ids.KindID(), Name: name, Hue: hue, Form: form,
		Motion: "float", Appearance: objects.DefaultOpenWorldAppearance(),
	}
	kind.Appearance.Shape = classicShape(len(world.Kinds))
	kind.Appearance.Motion = objects.OpenWorldMotionFloat
	world.Kinds = append(world.Kinds, kind)
	return kind
}

func (world *authoredWorld) addNode(kindID, name, text string, values map[string]any) *authoredNode {
	node := &authoredNode{
		ID: world.ids.NodeID(), KindID: kindID, Name: name, Text: text,
		Values: values, Status: objects.OpenWorldStatusOpen, X: 800, Y: 500,
	}
	world.Nodes = append(world.Nodes, node)
	return node
}

func (world *authoredWorld) addLink(from, to, label string) *authoredLink {
	// The state contract allows at most one link per ordered pair. Merge a
	// repeated pair instead of emitting a second link that validation rejects.
	for _, link := range world.Links {
		if link.From == from && link.To == to {
			if strings.TrimSpace(label) != "" {
				link.Label = label
			}
			return link
		}
	}
	link := &authoredLink{ID: world.ids.LinkID(), From: from, To: to, Label: label}
	world.Links = append(world.Links, link)
	return link
}

func (world *authoredWorld) toContent(experience *worldcap.Experience) objects.OpenWorldContent {
	content := objects.OpenWorldContent{
		Premise: world.Premise,
		Views: []string{
			objects.OpenWorldViewScene, objects.OpenWorldViewStage,
			objects.OpenWorldViewCards, objects.OpenWorldViewTimeline,
		},
		Presentation: &world.Presentation,
		Variables:    world.Variables,
		Experience:   experience,
	}
	kindIndex := make(map[string]int, len(world.Kinds))
	for index, kind := range world.Kinds {
		kindIndex[kind.ID] = index
		appearance := kind.Appearance
		content.Kinds = append(content.Kinds, objects.OpenWorldKindInput{
			Ref: kind.ID, Name: kind.Name, Hue: kind.Hue,
			Fields: kind.Fields, Appearance: &appearance, Bindings: kind.Bindings,
		})
	}
	for _, node := range world.Nodes {
		x, y := node.X, node.Y
		content.Nodes = append(content.Nodes, objects.OpenWorldNodeInput{
			Ref: node.ID, KindIndex: kindIndex[node.KindID], Name: node.Name,
			Text: node.Text, X: &x, Y: &y, Values: node.Values,
		})
	}
	for _, link := range world.Links {
		content.Links = append(content.Links, objects.OpenWorldLinkInput{
			FromIndex: indexOfNode(world, link.From), ToIndex: indexOfNode(world, link.To),
			Label: link.Label,
		})
	}
	return content
}

func (world *authoredWorld) toState(experience *worldcap.Experience) objects.OpenWorldState {
	views := world.Views
	if len(views) == 0 {
		views = []string{
			objects.OpenWorldViewScene, objects.OpenWorldViewStage,
			objects.OpenWorldViewCards, objects.OpenWorldViewTimeline,
		}
	}
	state := objects.OpenWorldState{
		Title: world.Title, Premise: world.Premise,
		Views:        append([]string(nil), views...),
		Rules:        append([]objects.OpenWorldRule(nil), world.Rules...),
		Tags:         append([]string(nil), world.Tags...),
		Presentation: world.Presentation, Variables: world.Variables,
		Experience: experience,
	}
	for _, kind := range world.Kinds {
		state.Kinds = append(state.Kinds, objects.OpenWorldKind{
			ID: kind.ID, Name: kind.Name, Hue: kind.Hue,
			Fields: kind.Fields, Appearance: kind.Appearance, Bindings: kind.Bindings,
		})
	}
	for _, node := range world.Nodes {
		state.Nodes = append(state.Nodes, objects.OpenWorldNode{
			ID: node.ID, KindID: node.KindID, Name: node.Name, Text: node.Text,
			Values: node.Values, Status: node.Status, X: node.X, Y: node.Y,
		})
	}
	for _, link := range world.Links {
		state.Links = append(state.Links, objects.OpenWorldLink{
			ID: link.ID, From: link.From, To: link.To, Label: link.Label,
		})
	}
	return state
}

func indexOfNode(world *authoredWorld, id string) int {
	for index, node := range world.Nodes {
		if node.ID == id {
			return index
		}
	}
	return 0
}

func bindingsForKind(kind *authoredKind) []objects.OpenWorldBinding {
	bindings := make([]objects.OpenWorldBinding, 0, 2)
	for _, field := range kind.Fields {
		if field.Type == objects.OpenWorldFieldNumber && len(bindings) < 1 {
			bindings = append(bindings, objects.OpenWorldBinding{
				Field: field.Key, Target: objects.OpenWorldBindingScale,
				InMin: 0, InMax: 10, OutMin: 0.7, OutMax: 1.5,
			})
		}
		if field.Type == objects.OpenWorldFieldFlag && len(bindings) < 2 {
			bindings = append(bindings, objects.OpenWorldBinding{
				Field: field.Key, Target: objects.OpenWorldBindingOpacity,
				InMin: 0, InMax: 1, OutMin: 0.5, OutMax: 1,
			})
		}
	}
	return bindings
}

func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic("worldseed: uuid: " + err.Error())
	}
	return id.String()
}

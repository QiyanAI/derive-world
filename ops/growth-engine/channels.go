// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	authorizationMaintainer = "maintainer-controlled"
	authorizationOwner      = "owner-account-required"
	authorizationNone       = "not-authorized"
)

// ChannelFile is the curated channel registry. It records what each channel
// accepts, not what the project is allowed to post. Authorization is explicit
// so automation cannot mistake a researched channel for a permitted one.
type ChannelFile struct {
	SchemaVersion int       `json:"schema_version"`
	UpdatedAt     string    `json:"updated_at"`
	Channels      []Channel `json:"channels"`
}

type Channel struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Audience      string               `json:"audience"`
	Rules         []string             `json:"rules"`
	Fits          []string             `json:"fits"`
	Cadence       string               `json:"cadence"`
	Authorization string               `json:"authorization"`
	Observed      []ChannelObservation `json:"observed"`
}

type ChannelObservation struct {
	Date    string `json:"date"`
	Summary string `json:"summary"`
	Outcome string `json:"outcome"`
}

func loadChannels(path string) (*ChannelFile, error) {
	var file ChannelFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported channel schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, channel := range file.Channels {
		if channel.ID == "" {
			return nil, fmt.Errorf("%s: channel with empty id", path)
		}
		if seen[channel.ID] {
			return nil, fmt.Errorf("%s: duplicate channel %q", path, channel.ID)
		}
		seen[channel.ID] = true
		switch channel.Authorization {
		case authorizationMaintainer, authorizationOwner, authorizationNone:
		default:
			return nil, fmt.Errorf("%s: channel %q has unknown authorization %q", path, channel.ID, channel.Authorization)
		}
		if len(channel.Rules) == 0 || len(channel.Fits) == 0 {
			return nil, fmt.Errorf("%s: channel %q must declare rules and fits", path, channel.ID)
		}
	}
	return &file, nil
}

func findChannel(file *ChannelFile, id string) (Channel, bool) {
	for _, channel := range file.Channels {
		if strings.EqualFold(channel.ID, id) {
			return channel, true
		}
	}
	return Channel{}, false
}

func channelFits(channel Channel, kind string) bool {
	for _, fit := range channel.Fits {
		if strings.EqualFold(fit, kind) {
			return true
		}
	}
	return false
}

// CheckChannel performs the preflight an automation must pass before it could
// even prepare a posting plan: the content kind must fit the channel and the
// channel must be maintainer-controlled. A channel that needs an owner account
// is returned as a hard error, never as an implicit permission.
func CheckChannel(channel Channel, kind string) error {
	if !channelFits(channel, kind) {
		return fmt.Errorf("channel %q does not accept content kind %q", channel.ID, kind)
	}
	if channel.Authorization != authorizationMaintainer {
		return fmt.Errorf("channel %q requires explicit owner authorization (%s); prepare the material but do not post",
			channel.ID, channel.Authorization)
	}
	return nil
}

func RenderChannelsMarkdown(file *ChannelFile) string {
	var builder strings.Builder
	builder.WriteString("# Growth channels\n\n")
	builder.WriteString("- Registry updated: " + file.UpdatedAt + "\n")
	builder.WriteString("- Authorization values: `maintainer-controlled` (repository-native, still subject to platform rules), `owner-account-required` (material only), `not-authorized` (do not use).\n\n")
	for _, channel := range file.Channels {
		builder.WriteString("## " + channel.Name + " (`" + channel.ID + "`)\n\n")
		builder.WriteString("- Audience: " + channel.Audience + "\n")
		builder.WriteString("- Authorization: `" + channel.Authorization + "`\n")
		builder.WriteString("- Cadence: " + channel.Cadence + "\n")
		builder.WriteString("- Accepts: " + strings.Join(channel.Fits, ", ") + "\n")
		builder.WriteString("- Rules:\n")
		for _, rule := range channel.Rules {
			builder.WriteString("  - " + rule + "\n")
		}
		builder.WriteString("- Observed results:\n")
		if len(channel.Observed) == 0 {
			builder.WriteString("  - None recorded. No outcome is claimed.\n")
		} else {
			for _, observed := range channel.Observed {
				builder.WriteString(fmt.Sprintf("  - %s: %s → %s\n", observed.Date, observed.Summary, observed.Outcome))
			}
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func runChannels(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "check" {
		return runChannelCheck(args[1:], stdout, stderr)
	}
	flags := newFlagSet("channels", stderr)
	path := flags.String("channels", "state/channels.json", "channel registry")
	asJSON := flags.Bool("json", false, "write JSON instead of Markdown")
	if err := flags.Parse(args); err != nil {
		return err
	}
	file, err := loadChannels(*path)
	if err != nil {
		return err
	}
	if *asJSON {
		encoded, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return err
		}
		_, err = stdout.Write(append(encoded, '\n'))
		return err
	}
	_, err = io.WriteString(stdout, RenderChannelsMarkdown(file))
	return err
}

func runChannelCheck(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("channels check", stderr)
	path := flags.String("channels", "state/channels.json", "channel registry")
	channelID := flags.String("channel", "", "channel id")
	kind := flags.String("kind", "", "content kind")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *channelID == "" || *kind == "" {
		return errors.New("--channel and --kind are required")
	}
	file, err := loadChannels(*path)
	if err != nil {
		return err
	}
	channel, ok := findChannel(file, *channelID)
	if !ok {
		return fmt.Errorf("unknown channel %q", *channelID)
	}
	if err := CheckChannel(channel, *kind); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "channel %q accepts kind %q and is maintainer-controlled\n", channel.ID, *kind)
	return nil
}

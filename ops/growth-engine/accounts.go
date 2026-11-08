// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlatformFile records researched platform rules. It is curated and dated;
// the AI may propose updates, but a human decides what enters this file.
type PlatformFile struct {
	SchemaVersion int        `json:"schema_version"`
	UpdatedAt     string     `json:"updated_at"`
	Platforms     []Platform `json:"platforms"`
}

type Platform struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Registration         string   `json:"registration"`
	AutomationPolicy     string   `json:"automation_policy"` // api_with_approval | manual_only | prohibited
	APIKind              string   `json:"api_kind"`
	PostingAPI           bool     `json:"posting_api"`
	CredentialEnv        string   `json:"credential_env,omitempty"`
	ContentRules         []string `json:"content_rules"`
	SecurityRequirements []string `json:"security_requirements"`
	OwnerOnlySteps       []string `json:"owner_only_steps"`
	SourceURLs           []string `json:"source_urls"`
	RetrievedAt          string   `json:"retrieved_at"`
	Notes                string   `json:"notes"`
}

// AccountFile is the Channel Account Manager state. It never contains a
// password, a one-time code, a recovery code, a session cookie or an API key.
type AccountFile struct {
	SchemaVersion int       `json:"schema_version"`
	UpdatedAt     string    `json:"updated_at"`
	Accounts      []Account `json:"accounts"`
}

type Account struct {
	Channel       string        `json:"channel"`
	Status        string        `json:"status"`
	Handle        string        `json:"handle,omitempty"`
	DisplayName   string        `json:"display_name,omitempty"`
	Bio           string        `json:"bio,omitempty"`
	Authorization Authorization `json:"authorization"`
	Security      Security      `json:"security"`
	API           AccountAPI    `json:"api"`
	OwnerSteps    []string      `json:"owner_steps"`
	LastChecked   string        `json:"last_checked,omitempty"`
	Notes         []string      `json:"notes"`
}

type Authorization struct {
	Scope     []string `json:"scope"`
	GrantedBy string   `json:"granted_by,omitempty"`
	GrantedAt string   `json:"granted_at,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type Security struct {
	TwoFactor                  bool   `json:"two_factor"`
	RecoveryEmail              bool   `json:"recovery_email"`
	RecoveryCodesStoredOffline bool   `json:"recovery_codes_stored_offline"`
	TokenScope                 string `json:"token_scope,omitempty"`
}

type AccountAPI struct {
	Available     bool   `json:"available"`
	Kind          string `json:"kind"`
	CredentialEnv string `json:"credential_env,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

var accountStatuses = map[string]bool{
	"owner-action-required": true,
	"prepared":              true,
	"active":                true,
	"suspended":             true,
	"retired":               true,
}

var authorizationPolicies = map[string]bool{
	"api_with_approval": true,
	"manual_only":       true,
	"prohibited":        true,
}

func loadPlatforms(path string) (*PlatformFile, error) {
	var file PlatformFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported platform schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, platform := range file.Platforms {
		if platform.ID == "" || platform.Name == "" {
			return nil, fmt.Errorf("%s: platform needs id and name", path)
		}
		if seen[platform.ID] {
			return nil, fmt.Errorf("%s: duplicate platform %q", path, platform.ID)
		}
		seen[platform.ID] = true
		if !authorizationPolicies[platform.AutomationPolicy] {
			return nil, fmt.Errorf("%s: platform %q has unknown automation policy %q", path, platform.ID, platform.AutomationPolicy)
		}
		if len(platform.SourceURLs) == 0 || platform.RetrievedAt == "" {
			return nil, fmt.Errorf("%s: platform %q needs source_urls and retrieved_at", path, platform.ID)
		}
		for _, source := range platform.SourceURLs {
			if !strings.HasPrefix(source, "https://") {
				return nil, fmt.Errorf("%s: platform %q source must be https", path, platform.ID)
			}
		}
	}
	return &file, nil
}

func findPlatform(file *PlatformFile, id string) (Platform, error) {
	for _, platform := range file.Platforms {
		if strings.EqualFold(platform.ID, id) {
			return platform, nil
		}
	}
	return Platform{}, fmt.Errorf("unknown platform %q", id)
}

func loadAccounts(path string) (*AccountFile, error) {
	var file AccountFile
	if err := readJSON(path, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported account schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, account := range file.Accounts {
		if account.Channel == "" {
			return nil, fmt.Errorf("%s: account with empty channel", path)
		}
		if seen[account.Channel] {
			return nil, fmt.Errorf("%s: duplicate account %q", path, account.Channel)
		}
		seen[account.Channel] = true
		if !accountStatuses[account.Status] {
			return nil, fmt.Errorf("%s: account %q has unknown status %q", path, account.Channel, account.Status)
		}
	}
	return &file, nil
}

func findAccount(file *AccountFile, channel string) (*Account, error) {
	for index := range file.Accounts {
		if strings.EqualFold(file.Accounts[index].Channel, channel) {
			return &file.Accounts[index], nil
		}
	}
	return nil, fmt.Errorf("unknown account %q", channel)
}

// AccountPreflight is the gate that must pass before any automated publish.
// It never inspects a secret; it checks recorded owner facts and scope.
func AccountPreflight(account *Account, scope string, now time.Time) error {
	if account.Status != "active" {
		return fmt.Errorf("account %q is %s, not active", account.Channel, account.Status)
	}
	authorized := false
	for _, item := range account.Authorization.Scope {
		if strings.EqualFold(item, scope) || item == "all" {
			authorized = true
		}
	}
	if !authorized {
		return fmt.Errorf("account %q has no %q authorization", account.Channel, scope)
	}
	if account.Authorization.GrantedBy == "" || account.Authorization.GrantedAt == "" {
		return fmt.Errorf("account %q authorization lacks granted_by or granted_at", account.Channel)
	}
	if account.Authorization.ExpiresAt != "" {
		expiry, err := time.Parse("2006-01-02", account.Authorization.ExpiresAt)
		if err != nil {
			return fmt.Errorf("account %q has an invalid authorization expiry", account.Channel)
		}
		if now.UTC().After(expiry.Add(24*time.Hour - time.Second)) {
			return fmt.Errorf("account %q authorization expired on %s", account.Channel, account.Authorization.ExpiresAt)
		}
	}
	if !account.Security.TwoFactor {
		return fmt.Errorf("account %q does not record two-factor authentication", account.Channel)
	}
	if !account.Security.RecoveryEmail {
		return fmt.Errorf("account %q does not record a recovery email", account.Channel)
	}
	return nil
}

func renderAccountsMarkdown(file *AccountFile) string {
	var builder strings.Builder
	builder.WriteString("# Channel accounts\n\n")
	builder.WriteString("- State updated: " + file.UpdatedAt + "\n")
	builder.WriteString("- Passwords, one-time codes, recovery codes and API keys are never stored here.\n\n")
	builder.WriteString("| Channel | Status | Handle | Scopes | 2FA | API |\n|---|---|---|---|---|---|\n")
	for _, account := range file.Accounts {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %t | %s |\n",
			account.Channel, account.Status, firstNonEmpty(account.Handle, "—"),
			firstNonEmpty(strings.Join(account.Authorization.Scope, ", "), "none"),
			account.Security.TwoFactor, account.API.Kind))
	}
	builder.WriteString("\n")
	for _, account := range file.Accounts {
		builder.WriteString("## " + account.Channel + "\n\n")
		builder.WriteString("- Status: " + account.Status + "\n")
		if account.DisplayName != "" {
			builder.WriteString("- Display name: " + account.DisplayName + "\n")
		}
		builder.WriteString("- API: " + account.API.Kind)
		if account.API.CredentialEnv != "" {
			builder.WriteString(" via `" + account.API.CredentialEnv + "`")
		}
		builder.WriteString("\n")
		builder.WriteString("- Owner steps:\n")
		for _, step := range account.OwnerSteps {
			builder.WriteString("  - " + step + "\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

// buildAccountKit prepares the truthful public presence for one platform. It
// uses verified facts only and records every step that a human must perform.
func buildAccountKit(platform Platform, facts *FactFile) (string, error) {
	byID := factMap(facts)
	need := func(id string) string {
		fact, ok := byID[id]
		if !ok {
			return ""
		}
		return fact.Statement
	}
	what := need("what-it-is")
	openSource := need("open-source")
	human := need("human-approval")
	notProduction := need("not-production")
	if what == "" || openSource == "" || human == "" || notProduction == "" {
		return "", errors.New("required facts are missing for the account kit")
	}
	var builder strings.Builder
	builder.WriteString("# Official account kit — " + platform.Name + "\n\n")
	builder.WriteString("Prepared, not registered. A human owns every account and every credential.\n\n")
	builder.WriteString("## Platform policy (researched)\n\n")
	builder.WriteString("- Automation policy: `" + platform.AutomationPolicy + "`\n")
	builder.WriteString("- API: " + platform.APIKind + "\n")
	builder.WriteString("- Retrieved: " + platform.RetrievedAt + "\n")
	builder.WriteString("- Sources:\n")
	for _, source := range platform.SourceURLs {
		builder.WriteString("  - " + source + "\n")
	}
	builder.WriteString("\n## Registration requirements\n\n")
	builder.WriteString(platform.Registration + "\n\n")
	builder.WriteString("## Name candidates\n\n")
	builder.WriteString("- Derive World\n- Derive World OSS\n- Derive World · Living Objects\n\n")
	builder.WriteString("Use one name consistently across channels. Do not imply affiliation with GitHub, DEV, Hacker News, Reddit or Product Hunt.\n\n")
	builder.WriteString("## Short bio (about 160 characters)\n\n")
	builder.WriteString("Open-source foundation for Living Objects: worlds people and their Personal AI create, change and derive together. Apache-2.0.\n\n")
	builder.WriteString("## Long bio\n\n")
	builder.WriteString(what + " " + human + " " + openSource + "\n\n")
	builder.WriteString("Do not say: production-ready, enterprise-grade, certified, funded, a chat app, a team workspace, or a plain AI game. Do not state user, star or download counts. " + notProduction + "\n\n")
	builder.WriteString("## Links\n\n")
	builder.WriteString("- Repository: https://github.com/QiyanAI/derive-world\n")
	builder.WriteString("- Showcase: https://github.com/QiyanAI/derive-world/blob/main/SHOWCASE.md\n")
	builder.WriteString("- Offline tour instructions: https://github.com/QiyanAI/derive-world/blob/main/showcase/tour/README.md\n")
	builder.WriteString("- Derive Relay: https://github.com/QiyanAI/derive-world/blob/main/examples/worlds/README.md\n\n")
	builder.WriteString("## Security checklist (owner)\n\n")
	for _, item := range platform.SecurityRequirements {
		builder.WriteString("- [ ] " + item + "\n")
	}
	builder.WriteString("- [ ] Store passwords, recovery codes and API keys only in the owner's password manager. Never send them to this repository or to the growth engine.\n\n")
	builder.WriteString("## Owner-only steps\n\n")
	for _, step := range platform.OwnerOnlySteps {
		builder.WriteString("- [ ] " + step + "\n")
	}
	builder.WriteString("\n## Content rules\n\n")
	for _, rule := range platform.ContentRules {
		builder.WriteString("- " + rule + "\n")
	}
	builder.WriteString("\n## Automation note\n\n")
	switch platform.AutomationPolicy {
	case "api_with_approval":
		builder.WriteString("A publish adapter exists. Live posting requires an active account record, `publish` scope, a matching approval artifact and a passing content verification. This kit authorizes nothing by itself.\n")
	case "manual_only":
		builder.WriteString("No automated publisher is implemented. The AI prepares material and a human performs the post on the platform.\n")
	case "prohibited":
		builder.WriteString("Automation is prohibited for this platform. Do not post from this project.\n")
	}
	builder.WriteString("\n## Facts used\n\n")
	for _, id := range []string{"what-it-is", "human-approval", "open-source", "not-production"} {
		if fact, ok := byID[id]; ok {
			builder.WriteString("- `" + id + "` — " + fact.Statement + "\n")
		}
	}
	builder.WriteString("\nThis kit has not been registered or posted anywhere.\n")
	return builder.String(), nil
}

func appendAudit(path string, entry map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func writeAccountState(path string, file *AccountFile) error {
	file.UpdatedAt = time.Now().UTC().Format("2006-01-02")
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(encoded, '\n'), true)
}

func renderOwnerTodo(file *AccountFile, platforms *PlatformFile) string {
	var builder strings.Builder
	builder.WriteString("# Owner action list — channel accounts\n\n")
	builder.WriteString("Only the account owner can complete these steps. The growth engine never asks for a password, one-time code or recovery code.\n\n")
	for _, account := range file.Accounts {
		platform, err := findPlatform(platforms, account.Channel)
		if err != nil {
			continue
		}
		blocked := account.Status != "active"
		var missing []string
		if !account.Security.TwoFactor {
			missing = append(missing, "Enable two-factor authentication")
		}
		if !account.Security.RecoveryEmail {
			missing = append(missing, "Set a recovery email")
		}
		if account.Authorization.GrantedBy == "" {
			missing = append(missing, "Record who grants publishing authorization and its scope")
		}
		if !blocked && len(missing) == 0 {
			continue
		}
		builder.WriteString("## " + platform.Name + " (`" + platform.ID + "`)\n\n")
		builder.WriteString("- Status: " + account.Status + "\n")
		steps := []string{}
		addStep := func(step string) {
			lower := strings.ToLower(step)
			for _, existing := range steps {
				if strings.Contains(strings.ToLower(existing), lower) {
					return
				}
			}
			steps = append(steps, step)
		}
		for _, step := range account.OwnerSteps {
			addStep(step)
		}
		for _, step := range platform.OwnerOnlySteps {
			addStep(step)
		}
		for _, step := range missing {
			addStep(step)
		}
		for _, step := range steps {
			builder.WriteString("- [ ] " + step + "\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("When a step is complete, record the non-secret facts with `accounts record`. Never record a password, code or key.\n")
	return builder.String()
}

func runAccounts(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: growth-engine accounts <list|show|prepare|check|preflight|record|owner-todo>")
	}
	switch args[0] {
	case "list":
		flags := newFlagSet("accounts list", stderr)
		path := flags.String("accounts", "state/accounts.json", "account state")
		asJSON := flags.Bool("json", false, "write JSON")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		file, err := loadAccounts(*path)
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
		_, err = io.WriteString(stdout, renderAccountsMarkdown(file))
		return err
	case "show":
		flags := newFlagSet("accounts show", stderr)
		path := flags.String("accounts", "state/accounts.json", "account state")
		channel := flags.String("channel", "", "channel id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		file, err := loadAccounts(*path)
		if err != nil {
			return err
		}
		if *channel == "" {
			return errors.New("--channel is required")
		}
		account, err := findAccount(file, *channel)
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(account, "", "  ")
		if err != nil {
			return err
		}
		_, err = stdout.Write(append(encoded, '\n'))
		return err
	case "prepare":
		flags := newFlagSet("accounts prepare", stderr)
		platformsPath := flags.String("platforms", "state/platforms.json", "platform registry")
		factsPath := flags.String("facts", "state/facts.json", "verified product facts")
		channel := flags.String("channel", "", "channel id")
		output := flags.String("out", "", "output path")
		force := flags.Bool("force", false, "replace an existing kit")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *channel == "" {
			return errors.New("--channel is required")
		}
		platforms, err := loadPlatforms(*platformsPath)
		if err != nil {
			return err
		}
		platform, err := findPlatform(platforms, *channel)
		if err != nil {
			return err
		}
		facts, err := loadFacts(*factsPath)
		if err != nil {
			return err
		}
		kit, err := buildAccountKit(platform, facts)
		if err != nil {
			return err
		}
		if *output == "" {
			*output = filepath.Join("accounts", "kits", platform.ID+".md")
		}
		return writeOutput(*output, []byte(kit), stdout, *force)
	case "check":
		flags := newFlagSet("accounts check", stderr)
		path := flags.String("accounts", "state/accounts.json", "account state")
		channel := flags.String("channel", "", "channel id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		file, err := loadAccounts(*path)
		if err != nil {
			return err
		}
		if *channel == "" {
			return errors.New("--channel is required")
		}
		account, err := findAccount(file, *channel)
		if err != nil {
			return err
		}
		missing := []string{}
		if account.Status != "active" {
			missing = append(missing, "account is "+account.Status)
		}
		if !account.Security.TwoFactor {
			missing = append(missing, "two-factor authentication is not recorded")
		}
		if !account.Security.RecoveryEmail {
			missing = append(missing, "recovery email is not recorded")
		}
		if len(account.Authorization.Scope) == 0 {
			missing = append(missing, "no authorization scope is recorded")
		}
		if len(missing) == 0 {
			fmt.Fprintf(stdout, "account %q has no recorded security gaps; live publishing still needs an approval artifact\n", account.Channel)
			return nil
		}
		for _, item := range missing {
			fmt.Fprintln(stdout, "MISSING "+item)
		}
		return errors.New("account is not ready for automated publishing")
	case "preflight":
		flags := newFlagSet("accounts preflight", stderr)
		path := flags.String("accounts", "state/accounts.json", "account state")
		channel := flags.String("channel", "", "channel id")
		scope := flags.String("scope", "publish", "authorization scope")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		file, err := loadAccounts(*path)
		if err != nil {
			return err
		}
		if *channel == "" {
			return errors.New("--channel is required")
		}
		account, err := findAccount(file, *channel)
		if err != nil {
			return err
		}
		if err := AccountPreflight(account, *scope, time.Now()); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "account %q passes preflight for scope %q\n", account.Channel, *scope)
		return nil
	case "record":
		flags := newFlagSet("accounts record", stderr)
		path := flags.String("accounts", "state/accounts.json", "account state")
		auditPath := flags.String("audit", "var/growth-engine/audit.jsonl", "append-only audit log")
		channel := flags.String("channel", "", "channel id")
		status := flags.String("status", "", "account status")
		handle := flags.String("handle", "", "public handle")
		scope := flags.String("scope", "", "comma-separated authorization scopes")
		authorizedBy := flags.String("authorized-by", "", "who grants publishing authorization")
		authorizedAt := flags.String("authorized-at", "", "authorization date YYYY-MM-DD")
		expiresAt := flags.String("expires-at", "", "authorization expiry YYYY-MM-DD")
		twoFactor := flags.Bool("two-factor", false, "two-factor authentication is enabled")
		recoveryEmail := flags.Bool("recovery-email", false, "recovery email is configured")
		tokenScope := flags.String("token-scope", "", "non-secret description of the API token scope")
		date := flags.String("date", "", "record date YYYY-MM-DD")
		write := flags.Bool("write", false, "write the updated state")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *channel == "" || *status == "" || *date == "" {
			return errors.New("--channel, --status and --date are required")
		}
		if !accountStatuses[*status] {
			return fmt.Errorf("unknown status %q", *status)
		}
		file, err := loadAccounts(*path)
		if err != nil {
			return err
		}
		account, err := findAccount(file, *channel)
		if err != nil {
			return err
		}
		account.Status = *status
		if *handle != "" {
			account.Handle = *handle
		}
		if *scope != "" {
			account.Authorization.Scope = splitList(*scope)
		}
		if *authorizedBy != "" {
			account.Authorization.GrantedBy = *authorizedBy
		}
		if *authorizedAt != "" {
			account.Authorization.GrantedAt = *authorizedAt
		}
		if *expiresAt != "" {
			account.Authorization.ExpiresAt = *expiresAt
		}
		account.Security.TwoFactor = *twoFactor
		account.Security.RecoveryEmail = *recoveryEmail
		if *tokenScope != "" {
			account.Security.TokenScope = *tokenScope
		}
		account.LastChecked = *date
		if *write {
			if err := writeAccountState(*path, file); err != nil {
				return err
			}
			if err := appendAudit(*auditPath, map[string]any{
				"date":    *date,
				"action":  "account.record",
				"channel": account.Channel,
				"status":  account.Status,
				"scopes":  account.Authorization.Scope,
			}); err != nil {
				return err
			}
		}
		fmt.Fprintf(stdout, "account %s recorded status=%s scopes=%v (written=%t)\n",
			account.Channel, account.Status, account.Authorization.Scope, *write)
		return nil
	case "owner-todo":
		flags := newFlagSet("accounts owner-todo", stderr)
		accountsPath := flags.String("accounts", "state/accounts.json", "account state")
		platformsPath := flags.String("platforms", "state/platforms.json", "platform registry")
		output := flags.String("out", "", "output path (default stdout)")
		force := flags.Bool("force", false, "replace an existing file")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		accounts, err := loadAccounts(*accountsPath)
		if err != nil {
			return err
		}
		platforms, err := loadPlatforms(*platformsPath)
		if err != nil {
			return err
		}
		return writeOutput(*output, []byte(renderOwnerTodo(accounts, platforms)), stdout, *force)
	default:
		return fmt.Errorf("unknown accounts subcommand %q", args[0])
	}
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

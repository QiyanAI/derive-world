// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Approval is an owner-authored, single-use authorization for one exact
// prepared payload. It never contains a credential.
type Approval struct {
	SchemaVersion int    `json:"schema_version"`
	Channel       string `json:"channel"`
	Action        string `json:"action"`
	ContentSHA256 string `json:"content_sha256"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ExpiresAt     string `json:"expires_at"`
}

type PublishLedger struct {
	SchemaVersion int            `json:"schema_version"`
	Entries       []PublishEntry `json:"entries"`
}

type PublishEntry struct {
	Channel       string `json:"channel"`
	ContentSHA256 string `json:"content_sha256"`
	ExternalID    string `json:"external_id"`
	URL           string `json:"url"`
	PublishedAt   string `json:"published_at"`
}

type PublishRequest struct {
	Title        string
	BodyMarkdown string
	Tags         []string
	CanonicalURL string
}

type PublishResult struct {
	ExternalID string
	URL        string
}

type publishAdapter interface {
	publish(ctx context.Context, request PublishRequest) (PublishResult, error)
}

func loadApproval(path string) (*Approval, error) {
	var approval Approval
	if err := readJSON(path, &approval); err != nil {
		return nil, err
	}
	if approval.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported approval schema_version %d", path, approval.SchemaVersion)
	}
	if approval.Channel == "" || approval.Action != "publish" || approval.ContentSHA256 == "" ||
		approval.ApprovedBy == "" || approval.ApprovedAt == "" || approval.ExpiresAt == "" {
		return nil, fmt.Errorf("%s: approval is incomplete", path)
	}
	return &approval, nil
}

func (approval *Approval) validate(channel, hash string, now time.Time) error {
	if !strings.EqualFold(approval.Channel, channel) {
		return fmt.Errorf("approval is for channel %q, not %q", approval.Channel, channel)
	}
	if approval.ContentSHA256 != hash {
		return errors.New("approval content hash does not match the prepared payload")
	}
	expiry, err := time.Parse("2006-01-02", approval.ExpiresAt)
	if err != nil {
		return errors.New("approval expires_at must be YYYY-MM-DD")
	}
	if now.UTC().After(expiry.Add(24*time.Hour - time.Second)) {
		return fmt.Errorf("approval expired on %s", approval.ExpiresAt)
	}
	return nil
}

func loadPublishLedger(path string) (*PublishLedger, error) {
	var ledger PublishLedger
	if err := readJSON(path, &ledger); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return &PublishLedger{SchemaVersion: 1, Entries: []PublishEntry{}}, nil
		}
		return nil, err
	}
	if ledger.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported ledger schema_version %d", path, ledger.SchemaVersion)
	}
	return &ledger, nil
}

func (ledger *PublishLedger) save(path string) error {
	encoded, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(encoded, '\n'), true)
}

func (ledger *PublishLedger) contains(channel, hash string) bool {
	for _, entry := range ledger.Entries {
		if strings.EqualFold(entry.Channel, channel) && entry.ContentSHA256 == hash {
			return true
		}
	}
	return false
}

var factMarkerPattern = regexp.MustCompile(`\[\[fact:[a-z0-9-]+\]\]`)

// prepareForPublication removes review-only scaffolding: the draft notice,
// fact markers and the trailing facts/channel appendix. Verification runs on
// the source draft before this transformation.
func prepareForPublication(content string) (string, error) {
	lines := strings.Split(content, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!-- Prepared draft") || strings.HasPrefix(trimmed, "<!-- AI draft:") {
			continue
		}
		if trimmed == "## Facts used" || trimmed == "## Channel" {
			break
		}
		kept = append(kept, line)
	}
	prepared := factMarkerPattern.ReplaceAllString(strings.Join(kept, "\n"), "")
	prepared = strings.TrimSpace(prepared)
	if !strings.HasPrefix(prepared, "# ") {
		return "", errors.New("draft must start with an H1 title")
	}
	return prepared + "\n", nil
}

func splitTitleAndBody(prepared string) (string, string) {
	lines := strings.SplitN(prepared, "\n", 2)
	title := strings.TrimSpace(strings.TrimPrefix(lines[0], "# "))
	body := ""
	if len(lines) == 2 {
		body = strings.TrimSpace(lines[1])
	}
	return title, body
}

func contentHash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\n\n" + body))
	return hex.EncodeToString(sum[:])
}

type devToPublisher struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (publisher *devToPublisher) publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	payload := map[string]any{"article": map[string]any{
		"title":         request.Title,
		"body_markdown": request.BodyMarkdown,
		"published":     true,
		"tags":          request.Tags,
	}}
	if request.CanonicalURL != "" {
		payload["article"].(map[string]any)["canonical_url"] = request.CanonicalURL
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return PublishResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(publisher.baseURL, "/")+"/api/articles", bytes.NewReader(body))
	if err != nil {
		return PublishResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("api-key", publisher.apiKey)
	httpRequest.Header.Set("User-Agent", "derive-world-growth-engine/1.0")
	response, err := publisher.client.Do(httpRequest)
	if err != nil {
		return PublishResult{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return PublishResult{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PublishResult{}, fmt.Errorf("dev.to returned %s: %s", response.Status, redact(truncateText(string(raw), 300)))
	}
	var parsed struct {
		ID  int    `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return PublishResult{}, fmt.Errorf("decode dev.to response: %w", err)
	}
	return PublishResult{ExternalID: fmt.Sprintf("%d", parsed.ID), URL: parsed.URL}, nil
}

type githubDiscussionPublisher struct {
	baseURL    string
	token      string
	repo       string
	categoryID string
	client     *http.Client
}

func (publisher *githubDiscussionPublisher) graphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	payload := map[string]any{"query": query, "variables": variables}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(publisher.baseURL, "/")+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+publisher.token)
	httpRequest.Header.Set("User-Agent", "derive-world-growth-engine/1.0")
	response, err := publisher.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned %s: %s", response.Status, redact(truncateText(string(raw), 300)))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Errors) > 0 {
		return nil, fmt.Errorf("GitHub GraphQL error: %s", envelope.Errors[0].Message)
	}
	return envelope.Data, nil
}

func (publisher *githubDiscussionPublisher) publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	owner, name, found := strings.Cut(publisher.repo, "/")
	if !found || owner == "" || name == "" {
		return PublishResult{}, errors.New("DGE_GITHUB_REPO must be owner/name")
	}
	data, err := publisher.graphQL(ctx,
		`query($owner:String!,$name:String!){repository(owner:$owner,name:$name){id}}`,
		map[string]any{"owner": owner, "name": name})
	if err != nil {
		return PublishResult{}, err
	}
	var repository struct {
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &repository); err != nil || repository.Repository.ID == "" {
		return PublishResult{}, errors.New("GitHub repository id was not returned")
	}
	data, err = publisher.graphQL(ctx,
		`mutation($repo:ID!,$category:ID!,$title:String!,$body:String!){createDiscussion(input:{repositoryId:$repo,categoryId:$category,title:$title,body:$body}){discussion{id url}}}`,
		map[string]any{"repo": repository.Repository.ID, "category": publisher.categoryID, "title": request.Title, "body": request.BodyMarkdown})
	if err != nil {
		return PublishResult{}, err
	}
	var created struct {
		CreateDiscussion struct {
			Discussion struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"discussion"`
		} `json:"createDiscussion"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return PublishResult{}, fmt.Errorf("decode GitHub response: %w", err)
	}
	if created.CreateDiscussion.Discussion.URL == "" {
		return PublishResult{}, errors.New("GitHub did not return a discussion URL")
	}
	return PublishResult{
		ExternalID: created.CreateDiscussion.Discussion.ID,
		URL:        created.CreateDiscussion.Discussion.URL,
	}, nil
}

func adapterBase(envName, fallback string) (string, error) {
	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return fallback, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an absolute URL", envName)
	}
	if parsed.Scheme != "https" && !loopbackHost(parsed.Hostname()) {
		return "", fmt.Errorf("%s must use https unless it is a loopback test endpoint", envName)
	}
	return strings.TrimRight(value, "/"), nil
}

func newPublishAdapter(platform Platform) (publishAdapter, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	switch platform.ID {
	case "devto":
		key := os.Getenv("DEVTO_API_KEY")
		if key == "" {
			return nil, errors.New("DEVTO_API_KEY is not set")
		}
		base, err := adapterBase("DGE_DEVTO_API_BASE", "https://dev.to")
		if err != nil {
			return nil, err
		}
		return &devToPublisher{baseURL: base, apiKey: key, client: client}, nil
	case "github":
		token := os.Getenv("DGE_GITHUB_TOKEN")
		if token == "" {
			return nil, errors.New("DGE_GITHUB_TOKEN is not set")
		}
		repo := os.Getenv("DGE_GITHUB_REPO")
		category := os.Getenv("DGE_GITHUB_DISCUSSION_CATEGORY_ID")
		if repo == "" || category == "" {
			return nil, errors.New("DGE_GITHUB_REPO and DGE_GITHUB_DISCUSSION_CATEGORY_ID must be set")
		}
		base, err := adapterBase("DGE_GITHUB_API_BASE", "https://api.github.com")
		if err != nil {
			return nil, err
		}
		return &githubDiscussionPublisher{
			baseURL: base, token: token, repo: repo, categoryID: category, client: client,
		}, nil
	default:
		return nil, fmt.Errorf("platform %q has no automated publisher", platform.ID)
	}
}

func runPublish(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("publish", stderr)
	channel := flags.String("channel", "", "platform id")
	file := flags.String("file", "", "verified draft file")
	approvalPath := flags.String("approval", "", "owner approval artifact")
	accountsPath := flags.String("accounts", "state/accounts.json", "account state")
	platformsPath := flags.String("platforms", "state/platforms.json", "platform registry")
	factsPath := flags.String("facts", "state/facts.json", "verified product facts")
	ledgerPath := flags.String("ledger", "var/growth-engine/published.json", "publish ledger")
	auditPath := flags.String("audit", "var/growth-engine/audit.jsonl", "append-only audit log")
	tags := flags.String("tags", "", "comma-separated platform tags")
	canonical := flags.String("canonical-url", "", "canonical URL")
	date := flags.String("date", "", "publish date YYYY-MM-DD")
	confirm := flags.Bool("confirm", false, "perform the live publish; default is a dry run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *channel == "" || *file == "" {
		return errors.New("--channel and --file are required")
	}
	if *date == "" {
		return errors.New("--date is required")
	}
	platforms, err := loadPlatforms(*platformsPath)
	if err != nil {
		return err
	}
	platform, err := findPlatform(platforms, *channel)
	if err != nil {
		return err
	}
	if platform.AutomationPolicy != "api_with_approval" || !platform.PostingAPI {
		return fmt.Errorf("platform %q is %s; prepare material and post manually", platform.ID, platform.AutomationPolicy)
	}
	accounts, err := loadAccounts(*accountsPath)
	if err != nil {
		return err
	}
	account, err := findAccount(accounts, platform.ID)
	if err != nil {
		return err
	}
	facts, err := loadFacts(*factsPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	verification := VerifyDraft(*file, string(content), facts)
	if !verification.OK {
		return errors.New(RenderVerificationText(verification))
	}
	prepared, err := prepareForPublication(string(content))
	if err != nil {
		return err
	}
	title, body := splitTitleAndBody(prepared)
	hash := contentHash(title, body)
	ledger, err := loadPublishLedger(*ledgerPath)
	if err != nil {
		return err
	}
	if ledger.contains(platform.ID, hash) {
		return fmt.Errorf("this exact payload was already published to %s", platform.ID)
	}
	accountError := AccountPreflight(account, "publish", time.Now())
	var approval *Approval
	var approvalError error
	if *approvalPath != "" {
		approval, err = loadApproval(*approvalPath)
		if err != nil {
			return err
		}
		approvalError = approval.validate(platform.ID, hash, time.Now())
	} else {
		approvalError = errors.New("no approval artifact supplied")
	}
	if !*confirm {
		preview := map[string]any{
			"channel":        platform.ID,
			"title":          title,
			"content_sha256": hash,
			"automation":     platform.AutomationPolicy,
			"account_ready":  accountError == nil,
			"account_error":  errorText(accountError),
			"approval_valid": approvalError == nil,
			"approval_error": errorText(approvalError),
			"would_publish":  false,
			"note":           "Dry run. Nothing was sent. Add --confirm with a valid approval artifact to publish.",
		}
		encoded, err := json.MarshalIndent(preview, "", "  ")
		if err != nil {
			return err
		}
		_, err = stdout.Write(append(encoded, '\n'))
		return err
	}
	if accountError != nil {
		return accountError
	}
	if approvalError != nil {
		return approvalError
	}
	adapter, err := newPublishAdapter(platform)
	if err != nil {
		return err
	}
	result, err := adapter.publish(context.Background(), PublishRequest{
		Title:        title,
		BodyMarkdown: body,
		Tags:         splitList(*tags),
		CanonicalURL: *canonical,
	})
	if err != nil {
		return err
	}
	ledger.Entries = append(ledger.Entries, PublishEntry{
		Channel:       platform.ID,
		ContentSHA256: hash,
		ExternalID:    result.ExternalID,
		URL:           result.URL,
		PublishedAt:   *date,
	})
	if err := ledger.save(*ledgerPath); err != nil {
		return err
	}
	if err := appendAudit(*auditPath, map[string]any{
		"date":           *date,
		"action":         "publish",
		"channel":        platform.ID,
		"content_sha256": hash,
		"external_id":    result.ExternalID,
		"url":            result.URL,
		"approved_by":    approval.ApprovedBy,
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "published to %s: %s (approval by %s)\n", platform.ID, result.URL, approval.ApprovedBy)
	return nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return redact(err.Error())
}

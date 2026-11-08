// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ApprovalKeyFile is the owner-registered public key set. Only public keys
// live here. The private key stays offline with the owner and never enters the
// repository or the engine's state.
type ApprovalKeyFile struct {
	SchemaVersion int           `json:"schema_version"`
	Keys          []ApprovalKey `json:"keys"`
}

type ApprovalKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	AddedAt   string `json:"added_at"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

// ApprovalPayload is the exact data an owner authorizes. It is signed with
// Ed25519; a plain JSON file without a valid signature is never accepted.
type ApprovalPayload struct {
	SchemaVersion int    `json:"schema_version"`
	ApprovalID    string `json:"approval_id"`
	Channel       string `json:"channel"`
	Action        string `json:"action"`
	ContentSHA256 string `json:"content_sha256"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ExpiresAt     string `json:"expires_at"`
	KeyID         string `json:"key_id"`
}

type SignedApproval struct {
	SchemaVersion int    `json:"schema_version"`
	ApprovalID    string `json:"approval_id"`
	Channel       string `json:"channel"`
	Action        string `json:"action"`
	ContentSHA256 string `json:"content_sha256"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`
	ExpiresAt     string `json:"expires_at"`
	KeyID         string `json:"key_id"`
	Signature     string `json:"signature"`
}

func (signed SignedApproval) payload() ApprovalPayload {
	return ApprovalPayload{
		SchemaVersion: signed.SchemaVersion,
		ApprovalID:    signed.ApprovalID,
		Channel:       signed.Channel,
		Action:        signed.Action,
		ContentSHA256: signed.ContentSHA256,
		ApprovedBy:    signed.ApprovedBy,
		ApprovedAt:    signed.ApprovedAt,
		ExpiresAt:     signed.ExpiresAt,
		KeyID:         signed.KeyID,
	}
}

func approvalPayloadBytes(payload ApprovalPayload) []byte {
	return []byte("derive-world-dge-approval-v1\n" +
		payload.ApprovalID + "\n" +
		payload.Channel + "\n" +
		payload.Action + "\n" +
		payload.ContentSHA256 + "\n" +
		payload.ApprovedBy + "\n" +
		payload.ApprovedAt + "\n" +
		payload.ExpiresAt + "\n" +
		payload.KeyID + "\n")
}

func loadApprovalKeys(path string) (*ApprovalKeyFile, error) {
	var file ApprovalKeyFile
	if err := readJSON(path, &file); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return &ApprovalKeyFile{SchemaVersion: 1, Keys: []ApprovalKey{}}, nil
		}
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported approval key schema_version %d", path, file.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, key := range file.Keys {
		if key.ID == "" {
			return nil, fmt.Errorf("%s: key with empty id", path)
		}
		if seen[key.ID] {
			return nil, fmt.Errorf("%s: duplicate key id %q", path, key.ID)
		}
		seen[key.ID] = true
		raw, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%s: key %q is not a valid Ed25519 public key", path, key.ID)
		}
	}
	return &file, nil
}

func (file *ApprovalKeyFile) find(id string) (ApprovalKey, error) {
	for _, key := range file.Keys {
		if key.ID == id {
			return key, nil
		}
	}
	return ApprovalKey{}, fmt.Errorf("approval key %q is not registered", id)
}

func loadSignedApproval(path string, keys *ApprovalKeyFile, channel, hash string, now time.Time) (*SignedApproval, error) {
	var signed SignedApproval
	if err := readJSON(path, &signed); err != nil {
		return nil, err
	}
	if err := verifySignedApproval(&signed, keys, channel, hash, now); err != nil {
		return nil, err
	}
	return &signed, nil
}

// verifySignedApproval is the independent owner boundary: without the private
// key, an AI cannot produce a signature that verifies against the registered
// public key.
func verifySignedApproval(signed *SignedApproval, keys *ApprovalKeyFile, channel, hash string, now time.Time) error {
	if signed.SchemaVersion != 2 {
		return fmt.Errorf("unsigned or legacy approval schema %d is not accepted", signed.SchemaVersion)
	}
	if signed.ApprovalID == "" || signed.ApprovedBy == "" || signed.ApprovedAt == "" || signed.ExpiresAt == "" {
		return errors.New("approval is missing an id, approver or date")
	}
	if signed.Action != "publish" {
		return fmt.Errorf("approval action %q is not publish", signed.Action)
	}
	if !strings.EqualFold(signed.Channel, channel) {
		return fmt.Errorf("approval is for channel %q, not %q", signed.Channel, channel)
	}
	if signed.ContentSHA256 != hash {
		return errors.New("approval content hash does not match the prepared payload")
	}
	key, err := keys.find(signed.KeyID)
	if err != nil {
		return err
	}
	if key.RevokedAt != "" {
		return fmt.Errorf("approval key %q was revoked on %s", key.ID, key.RevokedAt)
	}
	public, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return fmt.Errorf("approval key %q is invalid", key.ID)
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("approval signature is missing or malformed")
	}
	if !ed25519.Verify(ed25519.PublicKey(public), approvalPayloadBytes(signed.payload()), signature) {
		return errors.New("approval signature does not verify against the registered owner key")
	}
	expiry, err := time.Parse("2006-01-02", signed.ExpiresAt)
	if err != nil {
		return errors.New("approval expires_at must be YYYY-MM-DD")
	}
	if now.UTC().After(expiry.Add(24*time.Hour - time.Second)) {
		return fmt.Errorf("approval expired on %s", signed.ExpiresAt)
	}
	return nil
}

// ApprovalUseFile is the replay ledger. An approval id is consumed before the
// publish attempt; a retry needs a new owner signature.
type ApprovalUseFile struct {
	SchemaVersion int           `json:"schema_version"`
	Uses          []ApprovalUse `json:"uses"`
}

type ApprovalUse struct {
	ApprovalID    string `json:"approval_id"`
	Channel       string `json:"channel"`
	ContentSHA256 string `json:"content_sha256"`
	UsedAt        string `json:"used_at"`
}

func loadApprovalUses(path string) (*ApprovalUseFile, error) {
	var file ApprovalUseFile
	if err := readJSON(path, &file); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return &ApprovalUseFile{SchemaVersion: 1, Uses: []ApprovalUse{}}, nil
		}
		return nil, err
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: unsupported approval-use schema_version %d", path, file.SchemaVersion)
	}
	return &file, nil
}

func (file *ApprovalUseFile) contains(id string) bool {
	for _, use := range file.Uses {
		if use.ApprovalID == id {
			return true
		}
	}
	return false
}

func (file *ApprovalUseFile) save(path string) error {
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(encoded, '\n'), true)
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("private key is not base64")
	}
	switch len(decoded) {
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(decoded), nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	default:
		return nil, errors.New("private key has an unsupported size")
	}
}

func runApprovals(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: growth-engine approvals <keygen|sign|verify|list-keys>")
	}
	switch args[0] {
	case "keygen":
		flags := newFlagSet("approvals keygen", stderr)
		id := flags.String("id", "", "key id")
		privatePath := flags.String("private", "", "private key output path (outside the repository)")
		keysPath := flags.String("keys", "state/approval-keys.json", "public key registry")
		date := flags.String("date", "", "registration date YYYY-MM-DD")
		force := flags.Bool("force", false, "replace an existing private key or key id")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *privatePath == "" || *date == "" {
			return errors.New("--id, --private and --date are required")
		}
		if !*force {
			if _, err := os.Stat(*privatePath); err == nil {
				return fmt.Errorf("%s already exists; pass --force to replace it", *privatePath)
			}
		}
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		keys, err := loadApprovalKeys(*keysPath)
		if err != nil {
			return err
		}
		replaced := false
		for index := range keys.Keys {
			if keys.Keys[index].ID == *id {
				if !*force {
					return fmt.Errorf("key id %q already exists; pass --force to replace it", *id)
				}
				keys.Keys[index] = ApprovalKey{ID: *id, PublicKey: base64.StdEncoding.EncodeToString(public), AddedAt: *date}
				replaced = true
			}
		}
		if !replaced {
			keys.Keys = append(keys.Keys, ApprovalKey{ID: *id, PublicKey: base64.StdEncoding.EncodeToString(public), AddedAt: *date})
		}
		if err := os.MkdirAll(filepath.Dir(*privatePath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(*privatePath, []byte(base64.StdEncoding.EncodeToString(private)+"\n"), 0o600); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(keys, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFile(*keysPath, append(encoded, '\n'), true); err != nil {
			return err
		}
		fingerprint := sha256.Sum256(public)
		fmt.Fprintf(stdout, "registered public key %q (fingerprint %s); private key written to %s with mode 0600\n",
			*id, hex.EncodeToString(fingerprint[:8]), *privatePath)
		return nil
	case "sign":
		flags := newFlagSet("approvals sign", stderr)
		payloadPath := flags.String("file", "", "approval payload JSON")
		keyPath := flags.String("key", "", "owner private key path")
		output := flags.String("out", "", "signed approval output path")
		force := flags.Bool("force", false, "replace an existing signed approval")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *payloadPath == "" || *keyPath == "" || *output == "" {
			return errors.New("--file, --key and --out are required")
		}
		var payload ApprovalPayload
		if err := readJSON(*payloadPath, &payload); err != nil {
			return err
		}
		if payload.SchemaVersion != 2 || payload.Action != "publish" {
			return errors.New("payload must be schema 2 with action publish")
		}
		if payload.ApprovalID == "" || payload.Channel == "" || payload.ContentSHA256 == "" ||
			payload.ApprovedBy == "" || payload.ApprovedAt == "" || payload.ExpiresAt == "" || payload.KeyID == "" {
			return errors.New("payload is incomplete")
		}
		private, err := loadPrivateKey(*keyPath)
		if err != nil {
			return err
		}
		signature := ed25519.Sign(private, approvalPayloadBytes(payload))
		signed := SignedApproval{
			SchemaVersion: 2, ApprovalID: payload.ApprovalID, Channel: payload.Channel,
			Action: payload.Action, ContentSHA256: payload.ContentSHA256, ApprovedBy: payload.ApprovedBy,
			ApprovedAt: payload.ApprovedAt, ExpiresAt: payload.ExpiresAt, KeyID: payload.KeyID,
			Signature: base64.StdEncoding.EncodeToString(signature),
		}
		encoded, err := json.MarshalIndent(signed, "", "  ")
		if err != nil {
			return err
		}
		if err := writeOutput(*output, append(encoded, '\n'), stdout, *force); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "signed approval %s for %s\n", signed.ApprovalID, signed.Channel)
		return nil
	case "verify":
		flags := newFlagSet("approvals verify", stderr)
		file := flags.String("file", "", "signed approval")
		keysPath := flags.String("keys", "state/approval-keys.json", "public key registry")
		channel := flags.String("channel", "", "expected channel")
		hash := flags.String("hash", "", "expected content hash")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" || *channel == "" || *hash == "" {
			return errors.New("--file, --channel and --hash are required")
		}
		keys, err := loadApprovalKeys(*keysPath)
		if err != nil {
			return err
		}
		signed, err := loadSignedApproval(*file, keys, *channel, *hash, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "approval %s verifies: channel=%s approved_by=%s expires=%s\n",
			signed.ApprovalID, signed.Channel, signed.ApprovedBy, signed.ExpiresAt)
		return nil
	case "list-keys":
		flags := newFlagSet("approvals list-keys", stderr)
		keysPath := flags.String("keys", "state/approval-keys.json", "public key registry")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		keys, err := loadApprovalKeys(*keysPath)
		if err != nil {
			return err
		}
		for _, key := range keys.Keys {
			status := "active"
			if key.RevokedAt != "" {
				status = "revoked " + key.RevokedAt
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", key.ID, status, key.AddedAt)
		}
		return nil
	default:
		return fmt.Errorf("unknown approvals subcommand %q", args[0])
	}
}

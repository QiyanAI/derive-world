// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestApprovalSignatureVerification(t *testing.T) {
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	approvalPath := signApproval(t, dir, privatePath, "sig-1", "devto", "abc123")
	keys, err := loadApprovalKeys(keysPath)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := loadSignedApproval(approvalPath, keys, "devto", "abc123", time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("valid approval rejected: %v", err)
	}
	if signed.ApprovedBy != "owner" || signed.KeyID != "test-owner" {
		t.Fatalf("signed = %+v", signed)
	}
}

func TestApprovalRejectsWrongHashAndChannel(t *testing.T) {
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	approvalPath := signApproval(t, dir, privatePath, "sig-2", "devto", "abc123")
	keys, _ := loadApprovalKeys(keysPath)
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	if _, err := loadSignedApproval(approvalPath, keys, "devto", "other", now); err == nil {
		t.Fatal("wrong hash accepted")
	}
	if _, err := loadSignedApproval(approvalPath, keys, "github", "abc123", now); err == nil {
		t.Fatal("wrong channel accepted")
	}
}

func TestApprovalRejectsExpiredAndRevokedKeys(t *testing.T) {
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	approvalPath := signApproval(t, dir, privatePath, "sig-3", "devto", "abc123")
	keys, err := loadApprovalKeys(keysPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSignedApproval(approvalPath, keys, "devto", "abc123", time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expired approval accepted")
	}
	keys.Keys[0].RevokedAt = "2026-11-10"
	if _, err := loadSignedApproval(approvalPath, keys, "devto", "abc123", time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("revoked key accepted")
	}
}

func TestApprovalSignedByUnregisteredKeyIsRejected(t *testing.T) {
	dir := t.TempDir()
	privateA, keysA := makeOwnerKey(t, dir)
	approvalPath := signApproval(t, dir, privateA, "sig-4", "devto", "abc123")
	otherDir := t.TempDir()
	_, keysB := makeOwnerKey(t, otherDir)
	keys, err := loadApprovalKeys(keysB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSignedApproval(approvalPath, keys, "devto", "abc123", time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("approval signed by a different key was accepted")
	}
	_ = keysA
}

func TestApprovalKeygenRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	privatePath, keysPath := makeOwnerKey(t, dir)
	var stdout, stderr bytes.Buffer
	err := runApprovals([]string{
		"keygen", "--id", "test-owner", "--private", privatePath,
		"--keys", keysPath, "--date", "2026-11-08",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("keygen overwrote an existing key id without --force")
	}
	if _, statErr := os.Stat(privatePath); statErr != nil {
		t.Fatalf("private key missing: %v", statErr)
	}
}

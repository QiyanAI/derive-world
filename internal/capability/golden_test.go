// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// D06-04 freezes contract version 1.0. These hashes make an accidental schema
// drift fail CI in the same commit; changing any value requires a deliberate
// contract revision.
var frozenSchemaSHA256 = map[string]string{
	"content.structure.input.json":      "7672be7270934bd658866a5d9accdadf1b123bbaea0f5c530885271fe590c956",
	"content.structure.output.json":     "c888c861927200a4c68e90b10203b21a44a80a56409f5ad210d6e295ff8b6e43",
	"object.context.input.json":         "613cf4b2c8bb5d2ffae46e2744cce869cc260c391ef9853b5a12d7522814fcad",
	"object.context.output.json":        "364185d5019efc5a80385fbc4d509774d9e5cbbaab78b77087022171d0a56114",
	"object.derive.input.json":          "6de3800778ca0436e4ef45888ad8592a99512fcd172904fa07edc4d5275cf079",
	"object.derive.output.json":         "4f25af988b607d7069b60ea9cba828866040434cda93883ae69c47f12b51bba8",
	"object.propose_change.input.json":  "ac9f21fe52e2592d38a21e888661d70b1287d142d9ceb0be3a7e054b4b77b54d",
	"object.propose_change.output.json": "25fe62327a9b5e555e20034bef34334458cb55f13844591a871f869b7922c88d",
	"object.read.input.json":            "116a4e65a91b420567f86d0ce972c4963f71ea1f8f32ece2f0881334a1a1b4a2",
	"object.read.output.json":           "a364edef410879f2c9bea0892275280163ae07aec561e6851458c8832742a521",
}

func TestFrozenSchemaHashes(t *testing.T) {
	if ContractVersion != "1.0" {
		t.Fatalf("contract version = %q", ContractVersion)
	}
	entries, err := schemaFS.ReadDir("schemas")
	if err != nil {
		t.Fatalf("read schemas: %v", err)
	}
	if len(entries) != len(frozenSchemaSHA256) {
		t.Fatalf("schema file count = %d, want %d", len(entries), len(frozenSchemaSHA256))
	}
	for _, entry := range entries {
		expected, ok := frozenSchemaSHA256[entry.Name()]
		if !ok {
			t.Errorf("unexpected schema file %s", entry.Name())
			continue
		}
		raw, err := schemaFS.ReadFile("schemas/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != expected {
			t.Errorf("%s hash = %s, want %s", entry.Name(), got, expected)
		}
	}
}

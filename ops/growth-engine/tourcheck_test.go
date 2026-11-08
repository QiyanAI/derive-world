// SPDX-License-Identifier: Apache-2.0
package main

import "testing"

func TestCommittedTourIsOfflineAndLabelled(t *testing.T) {
	report, err := CheckTour("../../showcase/tour")
	if err != nil {
		t.Fatalf("check tour: %v", err)
	}
	if !report.OK {
		t.Fatalf("tour issues: %v", report.Issues)
	}
	if len(report.Checks) < 3 {
		t.Fatalf("too few checks ran: %v", report.Checks)
	}
}

package main

import (
	"encoding/json"
	"testing"
)

func TestGraphQLParsing(t *testing.T) {
	out, err := ghGraphQL(prQuery,
		"-f", "owner=lancedb",
		"-f", "name=lancedb",
	)
	if err != nil {
		t.Fatalf("graphql call failed: %v", err)
	}

	// Print raw response for debugging
	t.Logf("Raw response (first 2000 chars): %s", string(out[:min(len(out), 2000)]))

	var resp gqlResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	prs := resp.Data.Repository.PullRequests.Nodes
	if len(prs) == 0 {
		t.Fatal("no PRs returned")
	}

	for _, pr := range prs[:min(len(prs), 3)] {
		t.Logf("PR #%d: additions=%d, deletions=%d, title=%q", pr.Number, pr.Additions, pr.Deletions, pr.Title)
	}

	// At least one PR should have non-zero additions
	hasNonZero := false
	for _, pr := range prs {
		if pr.Additions > 0 || pr.Deletions > 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Error("all PRs have zero additions and deletions — parsing may be broken")
	}
}

func TestNeedsApprovalDetection(t *testing.T) {
	shas := fetchSHAsNeedingApproval("lancedb", "lancedb")
	t.Logf("SHAs needing approval: %d entries", len(shas))
	for sha := range shas {
		t.Logf("  %s", sha)
	}

	// PR #3192's SHA
	target := "02f2d16ce51cb06727eade1bb4512d1541840568"
	if !shas[target] {
		t.Errorf("expected SHA %s to be in needsApproval set", target[:12])
	}
}

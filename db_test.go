package main

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUpsertAndListNewPRs(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo:      "org/repo",
		Number:    1,
		Title:     "Add feature",
		Author:    "bob",
		Labels:    []string{"python"},
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now,
		Additions: 10,
		Deletions: 5,
		HeadSHA:   "abc123",
		CIStatus:  CIStatusPass,
		FetchedAt: now,
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatalf("upserting PR: %v", err)
	}

	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatalf("listing new PRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].Title != "Add feature" {
		t.Errorf("Title = %q, want %q", prs[0].Title, "Add feature")
	}
}

func TestNewPRsExcludesAuthoredByUser(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "My PR",
		Author: "alice", IsAuthor: true,
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 PRs (authored by user), got %d", len(prs))
	}
}

func TestNewPRsExcludesOldPRs(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Old PR",
		Author:    "bob",
		CreatedAt: now.Add(-15 * 24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 0 {
		t.Errorf("expected 0 PRs (too old), got %d", len(prs))
	}
}

func TestSnoozePR(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Feature",
		Author:    "bob",
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	// Snooze until the future — should be hidden
	if err := db.SnoozePR("org/repo", 1, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 0 {
		t.Errorf("expected snoozed PR to be hidden, got %d", len(prs))
	}

	// Snooze until the past — should reappear
	if err := db.SnoozePR("org/repo", 1, now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	prs, err = db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Errorf("expected expired snooze PR to reappear, got %d", len(prs))
	}
}

func TestDismissAndUndismiss(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Feature",
		Author:    "bob",
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	if err := db.DismissPR("org/repo", 1); err != nil {
		t.Fatal(err)
	}
	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 0 {
		t.Errorf("expected dismissed PR to be hidden, got %d", len(prs))
	}

	dismissed, err := db.ListDismissedPRs()
	if err != nil {
		t.Fatal(err)
	}
	if len(dismissed) != 1 {
		t.Fatalf("expected 1 dismissed PR, got %d", len(dismissed))
	}

	if err := db.UndismissPR("org/repo", 1); err != nil {
		t.Fatal(err)
	}
	prs, err = db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Errorf("expected undismissed PR to reappear, got %d", len(prs))
	}
}

func TestListReviewPRs(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	// PR where user is reviewer
	pr1 := PR{
		Repo: "org/repo", Number: 1, Title: "Review me",
		Author: "bob", IsReviewer: true,
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	// PR where user is assignee
	pr2 := PR{
		Repo: "org/repo", Number: 2, Title: "Assigned to me",
		Author: "carol", IsAssignee: true,
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	// PR where user is author — should be excluded
	pr3 := PR{
		Repo: "org/repo", Number: 3, Title: "My own PR",
		Author: "alice", IsAuthor: true, IsAssignee: true,
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}

	for _, pr := range []PR{pr1, pr2, pr3} {
		if err := db.UpsertPR(pr); err != nil {
			t.Fatal(err)
		}
	}

	prs, err := db.ListReviewPRs("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Errorf("expected 2 review PRs, got %d", len(prs))
	}
}

func TestReviewedPRMovesToReviewTab(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Feature",
		Author:    "bob",
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	// Before review: should be in New, not in Review
	newPRs, _ := db.ListNewPRs("alice", 10)
	reviewPRs, _ := db.ListReviewPRs("alice")
	if len(newPRs) != 1 {
		t.Errorf("expected 1 new PR before review, got %d", len(newPRs))
	}
	if len(reviewPRs) != 0 {
		t.Errorf("expected 0 review PRs before review, got %d", len(reviewPRs))
	}

	// After non-approval review: should move from New to Review
	if err := db.UpsertUserReview("org/repo", 1, now, "CHANGES_REQUESTED"); err != nil {
		t.Fatal(err)
	}
	newPRs, _ = db.ListNewPRs("alice", 10)
	reviewPRs, _ = db.ListReviewPRs("alice")
	if len(newPRs) != 0 {
		t.Errorf("expected 0 new PRs after review, got %d", len(newPRs))
	}
	if len(reviewPRs) != 1 {
		t.Errorf("expected 1 review PR after review, got %d", len(reviewPRs))
	}
}

func TestApprovedPRMovesToAwaitingMerge(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Feature",
		Author:    "bob",
		CIStatus:  CIStatusPass,
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
		Labels: []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	// Approve the PR
	if err := db.UpsertUserReview("org/repo", 1, now, "APPROVED"); err != nil {
		t.Fatal(err)
	}

	// Should be in Awaiting Merge (even with CI passing), not in New or Review
	newPRs, _ := db.ListNewPRs("alice", 10)
	reviewPRs, _ := db.ListReviewPRs("alice")
	awaitingPRs, _ := db.ListAwaitingMergePRs("alice")

	if len(newPRs) != 0 {
		t.Errorf("expected 0 new PRs after approval, got %d", len(newPRs))
	}
	if len(reviewPRs) != 0 {
		t.Errorf("expected 0 review PRs after approval, got %d", len(reviewPRs))
	}
	if len(awaitingPRs) != 1 {
		t.Errorf("expected 1 awaiting merge PR after approval, got %d", len(awaitingPRs))
	}
}

func TestHasUpdatesSinceReview(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	pr := PR{
		Repo: "org/repo", Number: 1, Title: "Feature",
		Author: "bob", IsReviewer: true,
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now, // updated now
		FetchedAt: now,
		Labels:    []string{},
	}
	if err := db.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}

	// No review yet — should count as has updates
	hasUpdates, err := db.HasUpdatesSinceReview("org/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasUpdates {
		t.Error("expected has updates when no review exists")
	}

	// Add a review from the past
	if err := db.UpsertUserReview("org/repo", 1, now.Add(-24*time.Hour), "APPROVED"); err != nil {
		t.Fatal(err)
	}
	hasUpdates, err = db.HasUpdatesSinceReview("org/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasUpdates {
		t.Error("expected has updates when PR updated after review")
	}

	// Add a review after the latest update
	if err := db.UpsertUserReview("org/repo", 1, now.Add(1*time.Hour), "APPROVED"); err != nil {
		t.Fatal(err)
	}
	hasUpdates, err = db.HasUpdatesSinceReview("org/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hasUpdates {
		t.Error("expected no updates when review is after latest update")
	}
}

func TestDeleteClosedPRs(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	for i := 1; i <= 3; i++ {
		pr := PR{
			Repo: "org/repo", Number: i, Title: "PR",
			Author:    "bob",
			CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now, FetchedAt: now,
			Labels: []string{},
		}
		if err := db.UpsertPR(pr); err != nil {
			t.Fatal(err)
		}
	}

	// Only PRs 1 and 3 are still open
	if err := db.DeleteClosedPRs("org/repo", []int{1, 3}); err != nil {
		t.Fatal(err)
	}

	prs, err := db.ListNewPRs("alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Errorf("expected 2 PRs after delete, got %d", len(prs))
	}
}

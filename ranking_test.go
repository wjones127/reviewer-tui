package main

import (
	"testing"
	"time"
)

func TestScorePR_CINeedsApproval(t *testing.T) {
	pr := PR{CIStatus: CIStatusNeedsApproval, CreatedAt: time.Now()}
	score := ScorePR(pr, nil, false)
	if score < 1000 {
		t.Errorf("expected score >= 1000 for needs_approval, got %d", score)
	}
}

func TestScorePR_HasUpdates(t *testing.T) {
	pr := PR{CreatedAt: time.Now()}
	withUpdates := ScorePR(pr, nil, true)
	withoutUpdates := ScorePR(pr, nil, false)
	if withUpdates-withoutUpdates != 500 {
		t.Errorf("expected 500 point difference for updates, got %d", withUpdates-withoutUpdates)
	}
}

func TestScorePR_AgeBonus(t *testing.T) {
	recent := PR{CreatedAt: time.Now().Add(-1 * time.Hour)}
	old := PR{CreatedAt: time.Now().Add(-72 * time.Hour)}
	veryOld := PR{CreatedAt: time.Now().Add(-300 * time.Hour)} // over 10 days

	recentScore := ScorePR(recent, nil, false)
	oldScore := ScorePR(old, nil, false)
	veryOldScore := ScorePR(veryOld, nil, false)

	if oldScore <= recentScore {
		t.Errorf("older PR should score higher: old=%d, recent=%d", oldScore, recentScore)
	}
	// Very old should be capped at 240
	if veryOldScore != 240 {
		t.Errorf("expected capped score of 240, got %d", veryOldScore)
	}
}

func TestScorePR_TagMatch(t *testing.T) {
	pr := PR{
		Labels:    []string{"python", "docs"},
		CreatedAt: time.Now(),
	}
	withTag := ScorePR(pr, []string{"python"}, false)
	withoutTag := ScorePR(pr, []string{"java"}, false)
	if withTag-withoutTag != 200 {
		t.Errorf("expected 200 point difference for tag match, got %d", withTag-withoutTag)
	}
}

func TestSortPRsByScore(t *testing.T) {
	now := time.Now()
	prs := []PR{
		{Repo: "org/r", Number: 1, CreatedAt: now.Add(-1 * time.Hour)},                                  // low score
		{Repo: "org/r", Number: 2, CreatedAt: now.Add(-1 * time.Hour), CIStatus: CIStatusNeedsApproval}, // high score
		{Repo: "org/r", Number: 3, CreatedAt: now.Add(-72 * time.Hour)},                                 // medium score (age)
	}
	updates := map[string]bool{}

	SortPRsByScore(prs, nil, updates)

	if prs[0].Number != 2 {
		t.Errorf("expected PR #2 first (needs approval), got #%d", prs[0].Number)
	}
	if prs[1].Number != 3 {
		t.Errorf("expected PR #3 second (oldest), got #%d", prs[1].Number)
	}
	if prs[2].Number != 1 {
		t.Errorf("expected PR #1 last, got #%d", prs[2].Number)
	}
}

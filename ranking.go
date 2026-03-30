package main

import (
	"fmt"
	"sort"
)

func ScorePR(pr PR, tags []string, hasUpdatesSinceReview bool) int {
	score := 0

	if pr.CIStatus == CIStatusNeedsApproval {
		score += 1000
	}

	if hasUpdatesSinceReview {
		score += 500
	}

	// Older PRs rank higher, capped at 10 days (240 hours)
	ageHours := int(pr.AgeHours())
	if ageHours > 240 {
		ageHours = 240
	}
	score += ageHours

	if pr.MatchesAnyTag(tags) {
		score += 200
	}

	return score
}

func SortPRsByScore(prs []PR, tags []string, updatesMap map[string]bool) {
	sort.Slice(prs, func(i, j int) bool {
		si := ScorePR(prs[i], tags, updatesMap[prKey(prs[i])])
		sj := ScorePR(prs[j], tags, updatesMap[prKey(prs[j])])
		if si != sj {
			return si > sj
		}
		// Tie-break: older PRs first
		return prs[i].CreatedAt.Before(prs[j].CreatedAt)
	})
}

func prKey(pr PR) string {
	return pr.Repo + "#" + fmt.Sprintf("%d", pr.Number)
}

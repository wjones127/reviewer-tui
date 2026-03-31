package main

import "time"

type CIStatus string

const (
	CIStatusPass          CIStatus = "pass"
	CIStatusFail          CIStatus = "fail"
	CIStatusPending       CIStatus = "pending"
	CIStatusNeedsApproval CIStatus = "needs_approval"
	CIStatusUnknown       CIStatus = ""
)

type PR struct {
	Repo                 string
	Number               int
	Title                string
	Body                 string
	Author               string
	AuthorAssociation    string
	IsBot                bool
	Labels               []string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Additions            int
	Deletions            int
	HeadSHA              string
	LastCommitAt         time.Time
	LastNonUserCommentAt time.Time
	CIStatus             CIStatus
	IsReviewer           bool
	IsAssignee           bool
	IsAuthor             bool
	IsDraft              bool
	ReviewDecision       string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	Mergeable            string // MERGEABLE, CONFLICTING, UNKNOWN
	FetchedAt            time.Time
}

func (p PR) IsNewContributor() bool {
	return p.AuthorAssociation == "FIRST_TIME_CONTRIBUTOR"
}

func (p PR) AgeHours() float64 {
	return time.Since(p.CreatedAt).Hours()
}

func (p PR) HasLabel(label string) bool {
	for _, l := range p.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func (p PR) MatchesAnyTag(tags []string) bool {
	for _, tag := range tags {
		if p.HasLabel(tag) {
			return true
		}
	}
	return false
}

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS pulls (
    repo              TEXT NOT NULL,
    number            INTEGER NOT NULL,
    title             TEXT NOT NULL,
    author            TEXT NOT NULL,
    author_association TEXT,
    labels            TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    additions         INTEGER,
    deletions         INTEGER,
    head_sha          TEXT,
    ci_status         TEXT,
    is_reviewer       INTEGER NOT NULL DEFAULT 0,
    is_assignee       INTEGER NOT NULL DEFAULT 0,
    is_author         INTEGER NOT NULL DEFAULT 0,
    is_draft          INTEGER NOT NULL DEFAULT 0,
    review_decision   TEXT,
    mergeable         TEXT,
    fetched_at        TEXT NOT NULL,
    PRIMARY KEY (repo, number)
);

CREATE TABLE IF NOT EXISTS user_reviews (
    repo         TEXT NOT NULL,
    number       INTEGER NOT NULL,
    reviewed_at  TEXT NOT NULL,
    review_state TEXT,
    PRIMARY KEY (repo, number)
);

CREATE TABLE IF NOT EXISTS pr_state (
    repo   TEXT NOT NULL,
    number INTEGER NOT NULL,
    state  TEXT NOT NULL,
    until  TEXT,
    PRIMARY KEY (repo, number)
);
`

type DB struct {
	db *sql.DB
}

func DefaultDBPath() string {
	return filepath.Join(configDir(), "data.db")
}

func OpenDB(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) UpsertPR(pr PR) error {
	labelsJSON, err := json.Marshal(pr.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}
	_, err = d.db.Exec(`
		INSERT INTO pulls (repo, number, title, author, author_association, labels,
			created_at, updated_at, additions, deletions, head_sha, ci_status,
			is_reviewer, is_assignee, is_author, is_draft, review_decision,
			mergeable, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo, number) DO UPDATE SET
			title = excluded.title,
			author = excluded.author,
			author_association = excluded.author_association,
			labels = excluded.labels,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			additions = excluded.additions,
			deletions = excluded.deletions,
			head_sha = excluded.head_sha,
			ci_status = excluded.ci_status,
			is_reviewer = excluded.is_reviewer,
			is_assignee = excluded.is_assignee,
			is_author = excluded.is_author,
			is_draft = excluded.is_draft,
			review_decision = excluded.review_decision,
			mergeable = excluded.mergeable,
			fetched_at = excluded.fetched_at`,
		pr.Repo, pr.Number, pr.Title, pr.Author, pr.AuthorAssociation,
		string(labelsJSON),
		pr.CreatedAt.Format(time.RFC3339), pr.UpdatedAt.Format(time.RFC3339),
		pr.Additions, pr.Deletions, pr.HeadSHA, string(pr.CIStatus),
		boolToInt(pr.IsReviewer), boolToInt(pr.IsAssignee), boolToInt(pr.IsAuthor),
		boolToInt(pr.IsDraft), pr.ReviewDecision, pr.Mergeable,
		pr.FetchedAt.Format(time.RFC3339),
	)
	return err
}

func (d *DB) UpsertUserReview(repo string, number int, reviewedAt time.Time, state string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_reviews (repo, number, reviewed_at, review_state)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (repo, number) DO UPDATE SET
			reviewed_at = CASE
				WHEN excluded.reviewed_at > user_reviews.reviewed_at
				THEN excluded.reviewed_at
				ELSE user_reviews.reviewed_at
			END,
			review_state = CASE
				WHEN excluded.reviewed_at > user_reviews.reviewed_at
				THEN excluded.review_state
				ELSE user_reviews.review_state
			END`,
		repo, number, reviewedAt.Format(time.RFC3339), state,
	)
	return err
}

// MarkNeedsApproval updates the CI status for any cached PR in this repo
// whose head_sha is in the needsApproval set.
func (d *DB) MarkNeedsApproval(repo string, needsApproval map[string]bool) error {
	if len(needsApproval) == 0 {
		return nil
	}
	query := "UPDATE pulls SET ci_status = ? WHERE repo = ? AND head_sha IN ("
	args := []any{string(CIStatusNeedsApproval), repo}
	first := true
	for sha := range needsApproval {
		if !first {
			query += ","
		}
		query += "?"
		args = append(args, sha)
		first = false
	}
	query += ")"
	_, err := d.db.Exec(query, args...)
	return err
}

func (d *DB) DeleteClosedPRs(repo string, openNumbers []int) error {
	if len(openNumbers) == 0 {
		_, err := d.db.Exec("DELETE FROM pulls WHERE repo = ?", repo)
		return err
	}
	// Build placeholders for the IN clause
	query := "DELETE FROM pulls WHERE repo = ? AND number NOT IN ("
	args := []any{repo}
	for i, n := range openNumbers {
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, n)
	}
	query += ")"
	_, err := d.db.Exec(query, args...)
	return err
}

// ListNewPRs returns PRs with no human reviews, not authored by the user,
// not snoozed/dismissed, and at most maxAgeDays old.
func (d *DB) ListNewPRs(user string, maxAgeDays int) ([]PR, error) {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).Format(time.RFC3339)
	rows, err := d.db.Query(`
		SELECT p.repo, p.number, p.title, p.author, p.author_association,
			p.labels, p.created_at, p.updated_at, p.additions, p.deletions,
			p.head_sha, p.ci_status, p.is_reviewer, p.is_assignee, p.is_author,
			p.is_draft, p.review_decision, p.mergeable, p.fetched_at
		FROM pulls p
		LEFT JOIN pr_state s ON p.repo = s.repo AND p.number = s.number
		WHERE p.is_author = 0
			AND p.is_draft = 0
			AND p.is_reviewer = 0
			AND p.is_assignee = 0
			AND p.created_at >= ?
			AND (s.state IS NULL
				OR (s.state = 'snoozed' AND s.until <= ?))
	`, cutoff, time.Now().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

// ListAwaitingMergePRs returns PRs that the user has personally approved
// but where CI is not yet passing (pending, failing, or needs approval).
func (d *DB) ListAwaitingMergePRs(user string) ([]PR, error) {
	rows, err := d.db.Query(`
		SELECT p.repo, p.number, p.title, p.author, p.author_association,
			p.labels, p.created_at, p.updated_at, p.additions, p.deletions,
			p.head_sha, p.ci_status, p.is_reviewer, p.is_assignee, p.is_author,
			p.is_draft, p.review_decision, p.mergeable, p.fetched_at
		FROM pulls p
		JOIN user_reviews r ON p.repo = r.repo AND p.number = r.number
		WHERE p.is_author = 0
			AND p.is_draft = 0
			AND r.review_state = 'APPROVED'
			AND p.ci_status != 'pass'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

// ListReviewPRs returns PRs where the user is reviewer or assignee,
// excluding user-authored PRs, not snoozed/dismissed.
func (d *DB) ListReviewPRs(user string) ([]PR, error) {
	rows, err := d.db.Query(`
		SELECT p.repo, p.number, p.title, p.author, p.author_association,
			p.labels, p.created_at, p.updated_at, p.additions, p.deletions,
			p.head_sha, p.ci_status, p.is_reviewer, p.is_assignee, p.is_author,
			p.is_draft, p.review_decision, p.mergeable, p.fetched_at
		FROM pulls p
		LEFT JOIN pr_state s ON p.repo = s.repo AND p.number = s.number
		WHERE p.is_author = 0
			AND p.is_draft = 0
			AND (p.is_reviewer = 1 OR p.is_assignee = 1)
			AND (s.state IS NULL
				OR (s.state = 'snoozed' AND s.until <= ?))
	`, time.Now().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

// HasUpdatesSinceReview reports whether the PR has been updated since the
// user's last review.
func (d *DB) HasUpdatesSinceReview(repo string, number int) (bool, error) {
	var hasUpdates bool
	err := d.db.QueryRow(`
		SELECT p.updated_at > COALESCE(r.reviewed_at, '1970-01-01T00:00:00Z')
		FROM pulls p
		LEFT JOIN user_reviews r ON p.repo = r.repo AND p.number = r.number
		WHERE p.repo = ? AND p.number = ?
	`, repo, number).Scan(&hasUpdates)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return hasUpdates, err
}

func (d *DB) SnoozePR(repo string, number int, until time.Time) error {
	_, err := d.db.Exec(`
		INSERT INTO pr_state (repo, number, state, until)
		VALUES (?, ?, 'snoozed', ?)
		ON CONFLICT (repo, number) DO UPDATE SET
			state = 'snoozed', until = excluded.until`,
		repo, number, until.Format(time.RFC3339),
	)
	return err
}

func (d *DB) DismissPR(repo string, number int) error {
	_, err := d.db.Exec(`
		INSERT INTO pr_state (repo, number, state, until)
		VALUES (?, ?, 'dismissed', NULL)
		ON CONFLICT (repo, number) DO UPDATE SET
			state = 'dismissed', until = NULL`,
		repo, number,
	)
	return err
}

func (d *DB) UndismissPR(repo string, number int) error {
	_, err := d.db.Exec(
		"DELETE FROM pr_state WHERE repo = ? AND number = ? AND state = 'dismissed'",
		repo, number,
	)
	return err
}

func (d *DB) ListDismissedPRs() ([]PR, error) {
	rows, err := d.db.Query(`
		SELECT p.repo, p.number, p.title, p.author, p.author_association,
			p.labels, p.created_at, p.updated_at, p.additions, p.deletions,
			p.head_sha, p.ci_status, p.is_reviewer, p.is_assignee, p.is_author,
			p.is_draft, p.review_decision, p.mergeable, p.fetched_at
		FROM pulls p
		JOIN pr_state s ON p.repo = s.repo AND p.number = s.number
		WHERE s.state = 'dismissed'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPRs(rows)
}

func scanPRs(rows *sql.Rows) ([]PR, error) {
	var prs []PR
	for rows.Next() {
		var pr PR
		var labelsJSON string
		var createdAt, updatedAt, fetchedAt string
		var ciStatus, authorAssoc, reviewDecision, mergeable sql.NullString
		var isReviewer, isAssignee, isAuthor, isDraft int

		err := rows.Scan(
			&pr.Repo, &pr.Number, &pr.Title, &pr.Author, &authorAssoc,
			&labelsJSON, &createdAt, &updatedAt, &pr.Additions, &pr.Deletions,
			&pr.HeadSHA, &ciStatus, &isReviewer, &isAssignee, &isAuthor,
			&isDraft, &reviewDecision, &mergeable, &fetchedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning PR row: %w", err)
		}

		if authorAssoc.Valid {
			pr.AuthorAssociation = authorAssoc.String
		}
		if ciStatus.Valid {
			pr.CIStatus = CIStatus(ciStatus.String)
		}
		if reviewDecision.Valid {
			pr.ReviewDecision = reviewDecision.String
		}
		if mergeable.Valid {
			pr.Mergeable = mergeable.String
		}
		pr.IsReviewer = isReviewer != 0
		pr.IsAssignee = isAssignee != 0
		pr.IsAuthor = isAuthor != 0
		pr.IsDraft = isDraft != 0

		if err := json.Unmarshal([]byte(labelsJSON), &pr.Labels); err != nil {
			pr.Labels = nil
		}
		pr.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		pr.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		pr.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)

		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

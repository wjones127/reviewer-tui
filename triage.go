package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// TriageResult holds the cached LLM triage output for a PR.
type TriageResult struct {
	HeadSHA string
	Summary string
	Effort  string
}

const triageSystemPrompt = `You are a code review triage assistant. For each PR below, estimate the effort needed to review it. Do not review the code — just estimate complexity and time.

Consider:
- Lines changed and number of files
- Whether changes are mechanical/repetitive (renames, linting, formatting) vs. novel logic
- Whether tests are included (less reviewer burden)
- Risk surface: API changes, schema changes, security-sensitive areas
- A 1000-line rename is simple; a 100-line algorithm is complex

You MUST respond with ONLY a JSON object (no markdown, no explanation) in this exact format:
{"prs":[{"id":"owner/repo#123","summary":"one sentence","effort":"2m"},...]}`

// maxDiffLines is the threshold of total changes (additions + deletions)
// above which we send file-level stats instead of the full diff.
const maxDiffLines = 400

// maxBatchChars limits prompt size per batch (~50k tokens ≈ 200k chars).
const maxBatchChars = 200_000

type triageResponse struct {
	PRs []triageResponseItem `json:"prs"`
}

type triageResponseItem struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Effort  string `json:"effort"`
}

// RunTriage identifies PRs needing triage, fetches diffs, calls the LLM,
// and stores results. Returns nil if there's nothing to do or if the claude
// CLI is not installed.
func RunTriage(db *DB) error {
	if _, err := exec.LookPath("claude"); err != nil {
		log.Printf("triage: claude CLI not found, skipping")
		return nil
	}

	prs, err := db.PRsNeedingTriage()
	if err != nil {
		return fmt.Errorf("listing PRs for triage: %w", err)
	}
	if len(prs) == 0 {
		log.Printf("triage: no PRs need triage")
		return nil
	}
	log.Printf("triage: %d PRs need triage", len(prs))

	// Fetch diffs/stats and bodies concurrently.
	type diffResult struct {
		key  string
		idx  int
		diff string
		body string
	}
	ch := make(chan diffResult, len(prs))
	sem := make(chan struct{}, 5) // limit concurrency
	for i, pr := range prs {
		go func(i int, pr PR) {
			sem <- struct{}{}
			defer func() { <-sem }()

			k := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
			diff, err := fetchDiffOrStat(pr.Repo, pr.Number, pr.Additions, pr.Deletions)
			if err != nil {
				diff = "(diff unavailable)"
			}
			var body string
			if pr.Body == "" {
				body, _ = fetchPRBody(pr.Repo, pr.Number)
			}
			ch <- diffResult{key: k, idx: i, diff: diff, body: body}
		}(i, pr)
	}

	diffs := make(map[string]string, len(prs))
	for range prs {
		r := <-ch
		diffs[r.key] = r.diff
		if r.body != "" {
			prs[r.idx].Body = r.body
		}
	}
	log.Printf("triage: fetched diffs for %d PRs", len(prs))

	batches := buildTriagePromptBatches(prs, diffs)
	log.Printf("triage: built %d batch(es)", len(batches))
	for i, prompt := range batches {
		log.Printf("triage: running batch %d (%d chars)", i+1, len(prompt))
		items, err := runTriageBatch(prompt)
		if err != nil {
			return fmt.Errorf("running triage batch: %w", err)
		}
		log.Printf("triage: batch %d returned %d results", i+1, len(items))
		if err := storeTriageResults(db, prs, items); err != nil {
			return err
		}
	}
	return nil
}

func fetchPRBody(repo string, number int) (string, error) {
	out, err := ghAPI("GET", fmt.Sprintf("/repos/%s/pulls/%d", repo, number))
	if err != nil {
		return "", err
	}
	var pr struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return "", err
	}
	return pr.Body, nil
}

func fetchDiffOrStat(repo string, number, additions, deletions int) (string, error) {
	args := []string{"pr", "diff", fmt.Sprintf("%d", number), "-R", repo}
	if additions+deletions > maxDiffLines {
		args = append(args, "--stat")
	}
	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gh pr diff: %s", string(exitErr.Stderr))
		}
		return "", err
	}
	return string(out), nil
}

func buildTriagePromptBatches(prs []PR, diffs map[string]string) []string {
	var batches []string
	var current strings.Builder
	current.WriteString(triageSystemPrompt)
	current.WriteString("\n\n")
	baseLen := current.Len()

	for _, pr := range prs {
		k := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
		var block strings.Builder
		fmt.Fprintf(&block, "<pr id=%q>\n", k)
		fmt.Fprintf(&block, "<title>%s</title>\n", pr.Title)
		if pr.Body != "" {
			fmt.Fprintf(&block, "<description>\n%s\n</description>\n", pr.Body)
		}
		diff := diffs[k]
		if diff != "" {
			fmt.Fprintf(&block, "<diff>\n%s\n</diff>\n", diff)
		}
		block.WriteString("</pr>\n\n")

		blockStr := block.String()

		// If adding this PR would exceed the batch limit, flush.
		if current.Len()+len(blockStr) > maxBatchChars && current.Len() > baseLen {
			batches = append(batches, current.String())
			current.Reset()
			current.WriteString(triageSystemPrompt)
			current.WriteString("\n\n")
		}
		current.WriteString(blockStr)
	}

	if current.Len() > baseLen {
		batches = append(batches, current.String())
	}
	return batches
}

func runTriageBatch(prompt string) ([]triageResponseItem, error) {
	cmd := exec.Command("claude", "-p",
		"--model", "haiku",
		"--output-format", "text",
	)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude: %w", err)
	}

	// The response may be wrapped in markdown code fences — strip them.
	text := strings.TrimSpace(string(out))
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	log.Printf("triage: raw response (%d bytes): %.500s", len(text), text)

	var resp triageResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("parsing triage response: %w (first 200 chars: %s)", err, text[:min(len(text), 200)])
	}
	return resp.PRs, nil
}

func storeTriageResults(db *DB, prs []PR, items []triageResponseItem) error {
	// Build a lookup from PR key to head SHA.
	shaMap := make(map[string]string, len(prs))
	for _, pr := range prs {
		shaMap[fmt.Sprintf("%s#%d", pr.Repo, pr.Number)] = pr.HeadSHA
	}

	for _, item := range items {
		headSHA, ok := shaMap[item.ID]
		if !ok {
			continue // LLM returned an unknown ID, skip
		}
		repo, number, err := parseTriageID(item.ID)
		if err != nil {
			continue
		}
		if err := db.UpsertTriage(repo, number, headSHA, item.Summary, item.Effort); err != nil {
			return fmt.Errorf("storing triage for %s: %w", item.ID, err)
		}
	}
	return nil
}

// parseTriageID splits "owner/repo#123" into repo and number.
func parseTriageID(id string) (string, int, error) {
	idx := strings.LastIndex(id, "#")
	if idx < 0 {
		return "", 0, fmt.Errorf("invalid triage ID: %s", id)
	}
	var number int
	if _, err := fmt.Sscanf(id[idx+1:], "%d", &number); err != nil {
		return "", 0, fmt.Errorf("invalid triage ID number: %s", id)
	}
	return id[:idx], number, nil
}

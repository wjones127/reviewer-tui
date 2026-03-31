# Agent Triage Design

## Goal

Estimate review effort for each PR using an LLM. Help the user triage by
showing how much work each PR will take to review, without reviewing the code
itself.

## Output

Per PR, the agent produces:
- **Estimated time** — e.g. "30s", "2m", "10m", "30m". Shown as a column.
- **One-line summary** — e.g. "Simple type fix." Shown in status bar on select.

## Model and invocation

Use Claude Haiku via the `claude` CLI in print mode:

```sh
claude -p --model haiku --output-format json --json-schema '...' < prompt.txt
```

The `--json-schema` flag enforces structured output, so we get reliable JSON
back without fragile parsing.

## Batching

Multiple PRs are sent in a single prompt to amortize system prompt cost and
reduce round-trips. Each PR is wrapped in an XML tag with its identifier.

Batch size is limited by input tokens. Target ~50k tokens per batch. If total
input exceeds that, split into multiple batches.

## Input per PR

Two tiers based on total changes (additions + deletions):

- **≤ 400 lines**: Full diff from `gh pr diff {number}`
- **> 400 lines**: File-level stats only from `gh pr diff {number} --stat`

This keeps the prompt small for large PRs where the full diff wouldn't fit
or be useful anyway.

## Prompt structure

```
System: You are a code review triage assistant. For each PR below, estimate
the effort needed to review it. Do not review the code — just estimate
complexity and time.

Consider:
- Lines changed and number of files
- Whether changes are mechanical/repetitive (renames, linting, formatting) vs. novel logic
- Whether tests are included (less reviewer burden)
- Risk surface: API changes, schema changes, security-sensitive areas
- A 1000-line rename is simple; a 100-line algorithm is complex

For each PR, provide:
- summary: one sentence describing what changed
- effort: estimated review time as a string ("30s", "1m", "2m", "5m", "10m", "30m", "1h")

<pr id="owner/repo#123">
<title>Fix Python SDK type annotation</title>
<description>
PR body/description here
</description>
<diff>
...full diff or stat summary...
</diff>
</pr>

<pr id="owner/repo#456">
...
</pr>
```

## JSON schema

```json
{
  "type": "object",
  "properties": {
    "prs": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id":      { "type": "string" },
          "summary": { "type": "string" },
          "effort":  { "type": "string" }
        },
        "required": ["id", "summary", "effort"]
      }
    }
  },
  "required": ["prs"]
}
```

## Caching

Results are cached in a new SQLite table:

```sql
CREATE TABLE pr_triage (
    repo     TEXT NOT NULL,
    number   INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    summary  TEXT NOT NULL,
    effort   TEXT NOT NULL,
    PRIMARY KEY (repo, number)
);
```

Cache key is `(repo, number)`, but we store `head_sha` to detect staleness.
A triage result is valid as long as the PR's `head_sha` hasn't changed. New
comments or reviews do NOT invalidate the cache — only new commits do. The
`head_sha` comes from the GraphQL response and is already stored in the
`pulls` table.

## Execution flow

1. After `FetchRepoPRs` completes and the DB is updated, identify PRs that
   need triage: any PR in `pulls` where either (a) no row exists in
   `pr_triage`, or (b) the `head_sha` in `pr_triage` doesn't match `pulls`.
2. For each PR needing triage, fetch the diff or stat depending on size.
3. Batch PRs into prompts under the ~50k token limit.
4. Shell out to `claude -p --model haiku --output-format json --json-schema`.
5. Parse the JSON response and upsert into `pr_triage`.
6. Send a message to the TUI to reload from DB, so the new triage data appears.

This runs in the background after a refresh. The TUI is usable immediately
with `...` in the Est. column for PRs that haven't been triaged yet.

## Display

- **Est. column** in the table: shows effort string ("2m", "10m") or "..."
  if pending.
- **Status bar**: when a PR is selected, show the one-line summary below the
  table (where status messages currently appear).

## Cost estimate

Haiku is ~$0.25/MTok input, $1.25/MTok output. A batch of 10 small PRs might
be ~5k tokens input, ~500 tokens output. That's roughly $0.002 per batch.
Even aggressive usage (50 PRs/day) would cost under $0.01/day.

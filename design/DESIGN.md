# Reviewer TUI — Design

## Overview

A TUI for OSS maintainers to triage and track pull request reviews across
multiple GitHub repositories.

## Configuration

A TOML config file with a commented template. Contains:

- **Repos**: List of GitHub repos to track (e.g. `lancedb/lancedb`,
  `lance-format/lance`). Always GitHub repos.
- **Tags**: Areas of interest for filtering (e.g. `python`, `rust`). PRs
  without matching tags still appear, but rank lower in priority.

Authentication and all GitHub interaction goes through the `gh` CLI.

## Tabs

### New PRs

PRs across configured repos that have no human reviews yet (bot reviews are
ignored). Capped at 10 days old.

### Review PRs

PRs where the user is assigned as reviewer or assignee, excluding PRs authored
by the user. Rows with updates (new commits or comments) since the user's last
review are visually highlighted (marker or color).

## Table Columns

Shared base columns across both tabs:

| Column         | Description                          |
|----------------|--------------------------------------|
| Repo           | Repository name                      |
| PR #           | Pull request number                  |
| Title          | PR title                             |
| Author         | PR author                            |
| CI Status      | Pass/fail indicator                  |
| +/- Lines      | Lines added/removed                  |
| Complexity     | Agent-generated: Low/Medium/Large    |
| Age            | Time since PR was opened             |

Tab-specific columns:

- **New PRs**: New contributor indicator, CI waiting on approval indicator.
- **Review PRs**: Has-updates-since-last-review marker.

## Priority Order

Fixed order, not user-sortable. From highest to lowest priority:

1. PRs needing CI approval (new contributors without write access).
2. PRs with updates since the user's last review (Review tab).
3. PRs open longer rank higher.
4. PRs matching the user's configured tags rank higher than non-matching.

## Actions

| Action               | Key | Behavior                                                    |
|----------------------|-----|-------------------------------------------------------------|
| Open in browser      |     | Opens the PR on GitHub                                      |
| Assign self          |     | Assign yourself as reviewer; PR moves from New to Review    |
| Approve CI runs      |     | Approve all pending workflow runs for the PR                |
| Snooze               | `s` | Hide for 2 days, regardless of new activity                 |
| Dismiss              |     | Hide from view; recoverable from a dismissed list           |

When assigning yourself, the screen stays on the current view so the user can
continue triaging the list without interruption.

## Agent Complexity Summary

A background agent evaluates each PR and assigns a complexity level:

- **Low**: Just a few lines, can be reviewed in seconds.
- **Medium**: Small feature or bug fix, reviewable in under 10 minutes.
- **Large**: Complex feature, many files changed, needs more than 10 minutes.

The agent result is cached and only re-runs when new commits or comments appear
since the last run. A loading indicator is shown in the table while the agent
is pending.

## Navigation

- **Up/Down arrows**: Move through the PR list.
- **Left/Right arrows**: Switch between tabs.
- **Single-letter keys**: Trigger actions (e.g. `s` for snooze).
- **Controls bar**: Always visible at the bottom of the screen.

## Data Freshness

Manual refresh via keybinding. No automatic polling.

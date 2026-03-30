# Overview

This is a TUI app for OSS maintainers to manage reviews and PRs.

## Workflows

### Finding new PRs

What do I want to find?

* New PRs that haven't received any reviews yet.
* Ones that are relevant to my areas of expertise.

What do I want to know about them?

* The title and description of the PR.
* Is this a new contributor?
* Are the CI checks waiting on approval? (Happens for new contributors who don't have write access to the repo.)
* Are the CI checks passing?
* How many lines of code are being added/removed?
* Qualitative:
    * How complex is the PR?
    * Are the changes tested?

### Checking in on existing PRs

Look at all PRs for which I am assinged as reviewer or assignee.

* Have there been updates since my last review?
* Is CI waiting on approval? Are the CI checks passing?


## Views

I want two tabs: "New PRs" and "Review PRs". These both show PRs that aren't mine.
They should be a tabular format, where I can up the up and down keys to navigate the list of PRs. When I select a PR, I can see more details about it, and have the option to open it in the browser.

### UI

The controls should be always shown at the bottom of the screen, and should be consistent across both tabs.

### Priority order

A key idea will be showing PRs in a priority order. We'll pay special attention to how we define this order and we'll likely tweak the algorithm as we get feedback from users. Some examples I have in mind:

* PRs that need approval of CI show up near top
* PRs that have been updated since the last time I reviewed them show up near the top
* PRs that have been open for a long time show up near the top

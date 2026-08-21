---
name: git-review
description: Request an interactive user review of local Git changes, worktree diffs, or proposed commits through the git-review web interface, and receive streaming reviewer comments. Use whenever you want the user to inspect diffs, review staged or unstaged changes, provide inline code feedback, or approve a proposed commit.
---

# `git-review`

Interactive web-based review tool for Git worktrees that streams reviewer comments back to the agent in real time.

## Workflow

### 1. Start Review

Run `git-review` with a proposed commit message summarizing your changes:

```bash
git-review --message "Short summary

Detailed explanation of changes."
```

- Prints the review URL to stderr. Share this URL with the user.
- Streams reviewer comments directly to stdout in real time.

### 2. Handle Streamed Comments

When the reviewer leaves inline feedback, `git-review` outputs comment blocks:

```
--- git-review comment added ---
id: <id>
file: <path>
anchor: right:<line>
area: unstaged
comment:
<reviewer comment>
--- end git-review comment ---
```

Event types include `comment added`, `comment resolved`, `comment reopened`, and `comment deleted`.

### 3. Apply Changes

1. Edit files to address the reviewer's feedback.
2. The review interface updates automatically when files are modified.
3. If disconnected or detached, re-run `git-review` to reattach (comments are preserved).

### 4. Finish Review

When review is complete or when exiting the review session, run:

```bash
git-review stop
```

This shuts down the review session and outputs any remaining unresolved comments:

```
--- git-review unresolved summary ---
<id> <path>: <comment>
--- end git-review unresolved summary ---
```

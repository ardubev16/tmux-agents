package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// gitState is where an agent's work stands, for its context card: its branch and, with the GitHub CLI, the pull
// request for it and that PR's CI.
type gitState struct {
	branch string
	pr     string // e.g. "#42 open"; empty without one (or without gh)
	ci     string // "✓", "✗", "…" (running), or empty without checks
}

func (g gitState) render() string {
	s := branchStyle.Render(g.branch)
	if g.pr != "" {
		s += dim.Render(" · ") + "PR " + g.pr
	}
	switch g.ci {
	case "✓":
		s += dim.Render(" · ") + "CI " + doneStyle.Render(g.ci)
	case "✗":
		s += dim.Render(" · ") + "CI " + warning.Render(g.ci)
	case "…":
		s += dim.Render(" · ") + "CI " + busy.Render(g.ci)
	}
	return s
}

type prEntry struct {
	at     time.Time
	pr, ci string
}

var prs sync.Map // "dir\tbranch" -> prEntry: gh asks GitHub, so at most once a minute per branch

// gitStatus is the branch checked out in dir and, through gh (cached for a minute), its pull request and CI. Run
// in the background: gh goes over the network.
func gitStatus(dir string) gitState {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return gitState{}
	}
	g := gitState{branch: strings.TrimSpace(string(out))}
	if g.branch == "" {
		return gitState{branch: "detached HEAD"}
	}
	key := dir + "\t" + g.branch
	if v, ok := prs.Load(key); ok && time.Since(v.(prEntry).at) < time.Minute {
		g.pr, g.ci = v.(prEntry).pr, v.(prEntry).ci
		return g
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", g.branch, "--json", "number,state,isDraft,statusCheckRollup")
	cmd.Dir = dir
	if out, err := cmd.Output(); err == nil { // no gh, no PR, not logged in: just the branch
		g.pr, g.ci = parsePR(out)
	}
	prs.Store(key, prEntry{time.Now(), g.pr, g.ci})
	return g
}

// parsePR reads gh pr view's JSON: the PR as "#42 open" (draft, merged, closed) and its checks summed up as ✓ (all
// passed or skipped), ✗ (any failed) or … (any still running).
func parsePR(out []byte) (pr, ci string) {
	var v struct {
		Number            int
		State             string
		IsDraft           bool
		StatusCheckRollup []struct{ Status, Conclusion, State string } // check runs: status+conclusion; statuses: state
	}
	if json.Unmarshal(out, &v) != nil || v.Number == 0 {
		return "", ""
	}
	state := strings.ToLower(v.State)
	if v.IsDraft && state == "open" {
		state = "draft"
	}
	if len(v.StatusCheckRollup) > 0 {
		ci = "✓"
	}
	for _, c := range v.StatusCheckRollup {
		switch result := c.Conclusion + c.State; {
		case result == "FAILURE" || result == "ERROR" || result == "CANCELLED" || result == "TIMED_OUT" ||
			result == "ACTION_REQUIRED" || result == "STARTUP_FAILURE":
			return fmt.Sprintf("#%d %s", v.Number, state), "✗"
		case c.Status != "" && c.Status != "COMPLETED", result == "PENDING" || result == "EXPECTED":
			ci = "…"
		}
	}
	return fmt.Sprintf("#%d %s", v.Number, state), ci
}

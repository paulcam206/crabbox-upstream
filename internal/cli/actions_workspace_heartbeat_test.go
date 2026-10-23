package cli

import (
	"testing"
	"time"
)

func TestActionsWorkspaceBindingToleratesConcurrentHeartbeatPublication(t *testing.T) {
	isolateRunTestUserDirs(t, t.TempDir())
	target := actionsWorkspaceTestSSH(t, `exec /bin/sh -c "$remote"`)
	repo := Repo{Root: t.TempDir(), RemoteURL: "https://github.com/example-org/source.git"}
	workspace := t.TempDir()
	runGit(t, workspace, "init", "-q")
	runGit(t, workspace, "remote", "add", "origin", "git@github.com:example-org/workflow.git")
	state := actionsHydrationState{Workspace: workspace, RunID: "123", EnvFile: workspace + "/environment"}
	const leaseID = "cbx_binding_heartbeat"
	if err := ClaimLeaseForRepoProvider(leaseID, "", "aws", repo.Root, time.Minute, false); err != nil {
		t.Fatal(err)
	}
	claim, exists, err := prepareActionsWorkspaceClaim(t.Context(), leaseID, repo)
	if err != nil || !exists {
		t.Fatalf("prepare claim: exists=%t err=%v", exists, err)
	}
	if _, err := UpdateLeaseClaimLabelsAndLastUsedIfUnchanged(leaseID, claim, map[string]string{"state": "running"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := bindActionsWorkspaceClaim(t.Context(), target, repo, GitHubRepo{Owner: "example-org", Name: "workflow"}, state, claim, exists); err != nil {
		t.Fatalf("heartbeat publication broke workspace binding: %v", err)
	}
	saved, _, err := ReadLeaseClaimWithPresence(leaseID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ActionsWorkspace == nil || saved.Labels["state"] != "running" {
		t.Fatalf("binding lost data: workspace=%v labels=%v", saved.ActionsWorkspace, saved.Labels)
	}
}

package sessionmanager

// Regression tests for the ungraceful-environment-crash recovery flow.
//
// Scenario: the host crashes while sessions are live (machine reboot, killed
// desktop, daemon SIGKILL). The worktrees and the durable session rows survive;
// the runtimes do not. On the next daemon boot the startup health check must
// settle those sessions as stopped-and-resumable (release + exited) instead of
// letting the reaper terminate them, the boot sweep must resume them in place,
// and a manual resume of an already-terminated session must recover it under
// its existing id — never spawn a replacement with a new number.
//
// Verified against the incident database: post-crash rows were
// is_terminated=1, activity=exited, runtime_released_launch_id='' (reaper
// termination), with session numbers climbing to kimchi-studio-141.

import (
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// seedCrashVictim seeds the durable state a session is in right after an
// ungraceful crash (daemon+runtime died): row present, workspace recorded,
// launch identity durable, no shutdown marker — whether or not a reaper pass
// already marked it terminated.
func seedCrashVictim(st *fakeStore, id domain.SessionID, terminated bool) domain.SessionRecord {
	rec := domain.SessionRecord{
		ID:        id,
		ProjectID: "mer",
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessClaudeCode,
		Metadata: domain.SessionMetadata{
			WorkspacePath:   "/ws/" + string(id),
			Branch:          "ao/" + string(id) + "/root",
			RuntimeHandleID: "h-" + string(id),
			RuntimeLaunchID: "launch-" + string(id),
			AgentSessionID:  "native-7",
		},
		Activity:     domain.Activity{State: domain.ActivityActive},
		IsTerminated: terminated,
	}
	st.sessions[id] = rec
	return rec
}

// TestBootAutoResume_RelaunchesCrashKilledSessionInPlace covers symptoms 1+2
// together: a session live at the crash must be settled (released+exited, NOT
// terminated) by the background reconcile, then auto-resumed by the boot sweep
// under the SAME id with a fresh runtime.
func TestBootAutoResume_RelaunchesCrashKilledSessionInPlace(t *testing.T) {
	m, st, rt, _ := newLifecycleManager()
	seedCrashVictim(st, "mer-1", false)

	if err := m.ReconcileBackground(ctx); err != nil {
		t.Fatalf("ReconcileBackground err = %v", err)
	}
	rec := st.sessions["mer-1"]
	if rec.IsTerminated {
		t.Fatal("startup settle terminated the session; user would have to archive -> restore by hand")
	}
	if rec.Metadata.RuntimeReleasedLaunchID == "" {
		t.Fatal("dead launch was not released; the next reaper probe would terminate the session")
	}

	if err := m.ResumeStartupSettledSessions(ctx); err != nil {
		t.Fatalf("ResumeStartupSettledSessions err = %v", err)
	}
	if rt.created == 0 {
		t.Fatal("crash-killed session was not auto-resumed: no runtime relaunch")
	}
	rec = st.sessions["mer-1"]
	if rec.IsTerminated {
		t.Fatal("session terminated after the boot sweep; want relaunched under the same id")
	}
	if rec.Metadata.RuntimeHandleID != "h1" {
		t.Fatalf("resumed runtime handle = %q, want the freshly created h1", rec.Metadata.RuntimeHandleID)
	}
}

// TestResumeAgent_WorksOnCrashTerminatedSession covers the manual path: once
// the reaper (or an older build) has terminated a crash victim, resume-agent
// must still recover THIS session via the restore path instead of refusing
// with ErrTerminated and pushing the client toward a new session number.
func TestResumeAgent_WorksOnCrashTerminatedSession(t *testing.T) {
	m, st, rt, _ := newLifecycleManager()
	seedCrashVictim(st, "mer-1", true)

	_, err := m.ResumeAgentWithMode(ctx, "mer-1")
	if err != nil {
		t.Fatalf("resume agent on crash-terminated session err = %v; want in-place recovery of the same session id", err)
	}
	if rt.created == 0 {
		t.Fatal("resume agent did not relaunch a runtime for the crash-terminated session")
	}
	if rec := st.sessions["mer-1"]; rec.IsTerminated {
		t.Fatal("session remains terminated after resume")
	}
}

// TestBootResumeSweepSkipsSessionsTheUserParkedBeforeTheCrash guards the
// auto-resume scope: only sessions proven dead at THIS boot are relaunched;
// sessions the user deliberately exited before the crash stay parked.
func TestBootResumeSweepSkipsSessionsTheUserParkedBeforeTheCrash(t *testing.T) {
	m, st, rt, _ := newLifecycleManager()
	rec := seedCrashVictim(st, "mer-1", false)
	rec.Activity = domain.Activity{State: domain.ActivityExited}
	rec.Metadata.RuntimeReleasedLaunchID = rec.Metadata.RuntimeLaunchID
	st.sessions["mer-1"] = rec

	if err := m.ResumeStartupSettledSessions(ctx); err != nil {
		t.Fatalf("ResumeStartupSettledSessions err = %v", err)
	}
	if rt.created != 0 {
		t.Fatal("sweep relaunched a session that was settled before the crash; user-exited sessions must stay parked")
	}
}

// TestCheckSessionHealth_ProbeErrorNeverSettles locks the boundary between
// this fix and the long-standing inconclusive-probe contract: a failed probe
// is not proof of death, so it must not settle (release/exit) or enter the
// boot resume sweep. A really-dead host reports a clean "not alive" instead
// and settles through the path the boot sweep resumes.
func TestCheckSessionHealth_ProbeErrorNeverSettles(t *testing.T) {
	m, st, rt, _ := newLifecycleManager()
	rec := seedCrashVictim(st, "mer-1", false)
	rt.aliveErr = errors.New("runtime probe unavailable")

	if err := m.checkSessionHealth(ctx, rec); err == nil {
		t.Fatal("inconclusive probe must keep the session checking, not settle")
	}
	got := st.sessions["mer-1"]
	if got.Metadata.RuntimeReleasedLaunchID != "" || got.Activity.State != domain.ActivityActive {
		t.Fatalf("inconclusive probe changed session facts: released=%q activity=%v",
			got.Metadata.RuntimeReleasedLaunchID, got.Activity.State)
	}
	if ids := m.drainStartupSettledDead(); len(ids) != 0 {
		t.Fatalf("inconclusive probe entered the boot resume sweep: %v", ids)
	}
}

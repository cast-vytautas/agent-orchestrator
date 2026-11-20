package kimchi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestNativeConversationIDRequiresCapturedKimchiSessionForTUI(t *testing.T) {
	p := &Plugin{}
	if id, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{
		ID: "ao-session-1", Metadata: map[string]string{},
	}, domain.SessionModeTUI, ""); err != nil || ok || id != "" {
		t.Fatalf("uncaptured TUI native id = %q ok=%v err=%v", id, ok, err)
	}
	tuiID, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{
		ID:       "ao-session-1",
		Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "019fc430-1234-7abc-8def-0123456789ab"},
	}, domain.SessionModeTUI, "")
	if err != nil || !ok || tuiID != "019fc430-1234-7abc-8def-0123456789ab" {
		t.Fatalf("captured TUI native id = %q ok=%v err=%v", tuiID, ok, err)
	}
	// Chat reports its identity through the provider conversation id; the
	// ACP session id and the TUI native id name the same transcript.
	chatID, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{},
		domain.SessionModeChat, tuiID)
	if err != nil || !ok || chatID != tuiID {
		t.Fatalf("Chat native id = %q ok=%v err=%v", chatID, ok, err)
	}
	if id, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{},
		domain.SessionModeChat, "  "); err != nil || ok || id != "" {
		t.Fatalf("blank Chat provider id = %q ok=%v err=%v", id, ok, err)
	}
}

func TestNativeConversationExistsRequiresPersistedKimchiTranscriptInCwdDir(t *testing.T) {
	p := &Plugin{}
	id := "019fc430-1234-7abc-8def-0123456789ab"
	home := t.TempDir()
	work := t.TempDir()
	env := map[string]string{"HOME": home, kimchiCodingAgentDirEnv: ""}
	ref := ports.SessionRef{ID: "ao-session-1", WorkspacePath: work}

	exists, err := p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("missing config root reported as persisted")
	}

	// A transcript under a different cwd directory does not count: Kimchi
	// resolves an id only within the exact cwd-keyed folder, and would mint
	// a fresh conversation with the same id if the file is elsewhere.
	otherDir := filepath.Join(home, ".config", "kimchi", "harness", "sessions", encodeKimchiCwdDir("/other/work"))
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "2026-08-08T10-00-00.000Z_"+id+".jsonl"),
		[]byte("{\"type\":\"session\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil || exists {
		t.Fatalf("transcript under a foreign cwd dir: exists=%v err=%v", exists, err)
	}

	sessionDir := filepath.Join(home, ".config", "kimchi", "harness", "sessions", encodeKimchiCwdDir(work))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil || exists {
		t.Fatalf("empty session dir: exists=%v err=%v", exists, err)
	}

	// An empty file is a reserved-but-never-written id, not history.
	empty := filepath.Join(sessionDir, "2026-08-08T10-00-00.000Z_"+id+".jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil || exists {
		t.Fatalf("empty transcript: exists=%v err=%v", exists, err)
	}

	if err := os.WriteFile(empty, []byte("{\"type\":\"session\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil || !exists {
		t.Fatalf("persisted transcript: exists=%v err=%v", exists, err)
	}

	// A directory entry for the id is not a resumable transcript.
	if err := os.Remove(empty); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sessionDir, id+".jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ref, id, env)
	if err != nil || exists {
		t.Fatalf("directory colliding with transcript name: exists=%v err=%v", exists, err)
	}
}

func TestNativeConversationExistsRejectsUnsafeIDsAndMissingWorkspace(t *testing.T) {
	p := &Plugin{}
	home := t.TempDir()
	env := map[string]string{"HOME": home, kimchiCodingAgentDirEnv: ""}

	// Path-escape-shaped ids must never reach the filesystem.
	exists, err := p.NativeConversationExists(context.Background(),
		ports.SessionRef{ID: "ao-session-1", WorkspacePath: t.TempDir()}, "../escape", env)
	if err != nil || exists {
		t.Fatalf("unsafe id: exists=%v err=%v", exists, err)
	}

	exists, err = p.NativeConversationExists(context.Background(),
		ports.SessionRef{ID: "ao-session-1"}, "019fc430-1234-7abc-8def-0123456789ab", env)
	if err == nil || exists {
		t.Fatalf("missing workspace: exists=%v err=%v", exists, err)
	}
}

func TestNativeConversationExistsRejectsConflictingConfigDir(t *testing.T) {
	p := &Plugin{}
	home := t.TempDir()
	env := map[string]string{"HOME": home, kimchiCodingAgentDirEnv: t.TempDir()}
	exists, err := p.NativeConversationExists(context.Background(),
		ports.SessionRef{ID: "ao-session-1", WorkspacePath: t.TempDir()},
		"019fc430-1234-7abc-8def-0123456789ab", env)
	if err == nil || exists {
		t.Fatalf("conflicting KIMCHI_CODING_AGENT_DIR: exists=%v err=%v", exists, err)
	}
}

package kimchi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.AgentInterfaceHandoff = (*Plugin)(nil)
var _ ports.AgentInterfaceHandoffHistoryProbe = (*Plugin)(nil)

// NativeConversationID bridges Kimchi's terminal resume id and its ACP Chat
// session id. Both surfaces persist conversations in the same pi-style store —
// <config>/sessions/<cwd-keyed-dir>/<iso>_<id>.jsonl — so the id the TUI
// reports through its session hooks and the id the ACP driver reports as the
// provider conversation id name the same native conversation. Kimchi's own
// loader resolves an id only within the cwd-keyed directory (see
// defaultSessionLoader in kimchi's src/modes/acp/server.ts and pi's
// --session <path|id> resolution), so no cross-surface translation is needed.
func (p *Plugin) NativeConversationID(
	ctx context.Context,
	session ports.SessionRef,
	currentMode domain.SessionMode,
	providerConversationID string,
) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if domain.NormalizeSessionMode(currentMode) == domain.SessionModeChat {
		id := strings.TrimSpace(providerConversationID)
		return id, id != "", nil
	}
	id := strings.TrimSpace(session.Metadata[ports.MetadataKeyAgentSessionID])
	return id, id != "", nil
}

// NativeConversationExists proves that a native id resolves to transcript
// history Kimchi can actually replay. The check is scoped to the exact
// cwd-keyed directory Kimchi will load from: an id whose transcript lives
// under a different cwd directory is not resumable — Kimchi's ACP loader
// would silently mint a fresh conversation with the same id — so a
// whole-tree scan would false-positive. A non-empty regular transcript file
// mirrors Codex's rollout probe: pi delays writing a new session until its first assistant
// response, so size > 0 distinguishes a persisted conversation from a
// reserved-but-never-written id. A missing directory or id reports the
// conversation as non-existent rather than erroring: Session Manager then
// decides whether durable freshness proof allows a fresh handoff.
func (p *Plugin) NativeConversationExists(
	ctx context.Context,
	session ports.SessionRef,
	nativeConversationID string,
	env map[string]string,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// An id Kimchi would reject is not a resumable conversation; mirroring
	// Codex's probe, it reports non-existence rather than erroring.
	id := strings.TrimSpace(nativeConversationID)
	if !kimchiSessionIDPattern.MatchString(id) {
		return false, nil
	}
	if strings.TrimSpace(session.WorkspacePath) == "" {
		return false, errors.New("kimchi: workspace path required for native conversation lookup")
	}
	configDir, err := p.NativeSessionConfigDir(ctx, env)
	if err != nil {
		return false, err
	}
	sessionDir := filepath.Join(configDir, "sessions", encodeKimchiCwdDir(session.WorkspacePath))
	bare := id + ".jsonl"
	suffix := "_" + id + ".jsonl"
	entries, err := os.ReadDir(sessionDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kimchi: scan native conversation dir: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		name := entry.Name()
		if name != bare && !strings.HasSuffix(name, suffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return false, fmt.Errorf("kimchi: inspect native conversation: %w", err)
		}
		if info.Mode().IsRegular() && info.Size() > 0 {
			return true, nil
		}
	}
	return false, nil
}

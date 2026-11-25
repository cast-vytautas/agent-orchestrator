package kimchi

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The fixtures below are compressed from a real 1.34.3 capture of the pi-based
// TUI (startup banner, idle composer, in-flight spinner, and a live
// Questionnaire dialog).
const kimchiIdleFrame = `┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                                        │ Kimchi's special:                       │
│               █▀  █  █ ▀█▀ █▄ ▄█ ▄▀▀ █  █ ▀█▀          │ Use /ferment to hand off a large task   │
│          v1.34.3 · /private/tmp/ao-kimchi-probe        │                                         │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
────────────────────────────────────────────────────────────────────────────────────────────────────
❯  ask anything or type / for commands
────────────────────────────────────────────────────────────────────────────────────────────────────
default → shift+tab · auto → ctrl+p · thinking:auto · 0% ctx · ↑0 ↓0                  / for commands`

const kimchiQuestionnaireFrame = ` ▍ Thought for 1s
────────────────────────────────────────────────────────────────────────────────────────────────────
 ▍ ○ Questionnaire What is 1+1?
────────────────────────────────────────────────────────────────────────────────────────────────────
Tip: Press shift+tab to change permissions mode.
────────────────────────────────────────────────────────────────────────────────────────────────────
 Quick math check
 What is 1+1?
> 1. 2
  2. 3
 ↑↓ navigate • 1-9 pick • Enter select • Esc cancel
────────────────────────────────────────────────────────────────────────────────────────────────────
plan → shift+tab · auto → ctrl+p · thinking:auto · 2% ctx · ↑15k ↓129                 / for commands
 ▍ ● Questionnaire What is 1+1? 128ms`

const kimchiBusyFrame = `────────────────────────────────────────────────────────────────────────────────────────────────────
 / Chopping..
 ▍ ○ Questionnaire What is 1+1
 ▍ ○ Questionnaire What is 1+1?
 - Chopping..
 ▍ Thought for 1s
────────────────────────────────────────────────────────────────────────────────────────────────────
 ▍ ○ Questionnaire What is 1+1?
────────────────────────────────────────────────────────────────────────────────────────────────────
Tip: Press shift+tab to change permissions mode.`

const kimchiDraftFrame = `────────────────────────────────────────────────────────────────────────────────────────────────────
❯  fix the flaky interface-switch test
────────────────────────────────────────────────────────────────────────────────────────────────────
default → shift+tab · auto → ctrl+p · thinking:auto · 0% ctx · ↑0 ↓0                  / for commands`

// An answered Questionnaire: its rows stayed in scrollback, but the composer
// and a resumed spinner below the footer prove the decision no longer waits.
const kimchiAnsweredQuestionnaireFrame = ` Quick math check
 What is 1+1?
> 1. 2
  2. 3
 ↑↓ navigate • 1-9 pick • Enter select • Esc cancel
────────────────────────────────────────────────────────────────────────────────────────────────────
 \ Chopping..
────────────────────────────────────────────────────────────────────────────────────────────────────
default → shift+tab · auto → ctrl+p · thinking:auto · 2% ctx · ↑15k ↓129                / for commands`

func TestInspectTerminalSurface(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   ports.TerminalSurfaceObservation
	}{
		{
			name:   "empty capture stays unknown",
			output: "",
			want:   ports.TerminalSurfaceObservation{Work: ports.TerminalSurfaceWorkUnknown, Composer: ports.TerminalComposerUnknown},
		},
		{
			name:   "initial banner with untouched composer proves a conversation has not started",
			output: kimchiIdleFrame,
			want: ports.TerminalSurfaceObservation{
				Work:                         ports.TerminalSurfaceWorkIdle,
				Composer:                     ports.TerminalComposerEmpty,
				NativeConversationNotStarted: true,
			},
		},
		{
			name:   "a composer without the banner proves idle but not freshness",
			output: kimchiDraftFrame,
			want: ports.TerminalSurfaceObservation{
				Work:     ports.TerminalSurfaceWorkIdle,
				Composer: ports.TerminalComposerDraft,
			},
		},
		{
			name:   "a live Questionnaire waits for input",
			output: kimchiQuestionnaireFrame,
			want: ports.TerminalSurfaceObservation{
				Work:     ports.TerminalSurfaceWorkWaitingInput,
				Composer: ports.TerminalComposerUnknown,
			},
		},
		{
			name:   "a live spinner proves active work",
			output: kimchiBusyFrame,
			want: ports.TerminalSurfaceObservation{
				Work:     ports.TerminalSurfaceWorkActive,
				Composer: ports.TerminalComposerUnknown,
			},
		},
		{
			name:   "an answered Questionnaire in scrollback does not outrank a resumed spinner",
			output: kimchiAnsweredQuestionnaireFrame,
			want: ports.TerminalSurfaceObservation{
				Work:     ports.TerminalSurfaceWorkActive,
				Composer: ports.TerminalComposerUnknown,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&Plugin{}).InspectTerminalSurface(tt.output)
			if got != tt.want {
				t.Fatalf("InspectTerminalSurface() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

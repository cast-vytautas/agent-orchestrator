package kimchi

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/terminalui"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.TerminalSurfaceInspector = (*Plugin)(nil)

const kimchiTerminalSurfaceLookbackLines = 14

// kimchiComposerChromeLabels names the hint text Kimchi paints into the current
// composer row in a muted gray. Muted truecolor is not SGR dim, so without this
// label the placeholder would read as a human-authored draft (verified against
// the 1.34.x TUI: the row is "❯" + "ask anything or type / for commands").
var kimchiComposerChromeLabels = []string{"ask anything or type / for commands"}

var kimchiVersionBannerPattern = regexp.MustCompile(`\bv\d+\.\d+\.\d+ · /`)

// InspectTerminalSurface reports independent work and composer facts from
// Kimchi's pi-based TUI. The TUI replaces the composer with an option list
// while a Questionnaire waits, so a hidden composer can never prove idle; only
// the decision frame's own footer does.
func (p *Plugin) InspectTerminalSurface(output string) ports.TerminalSurfaceObservation {
	composer := terminalui.LastBorderedPromptComposerState(output, "❯", kimchiComposerChromeLabels...)
	observation := ports.TerminalSurfaceObservation{Composer: kimchiComposerState(composer)}
	switch {
	case kimchiDecisionFrameVisible(output):
		// A Questionnaire (pi selection dialog) waits on the human. The composer
		// is not rendered while it is open, so the composer fact is meaningless.
		observation.Work = ports.TerminalSurfaceWorkWaitingInput
		observation.Composer = ports.TerminalComposerUnknown
	case kimchiActiveFrameVisible(output):
		observation.Work = ports.TerminalSurfaceWorkActive
	case observation.Composer != ports.TerminalComposerUnknown:
		observation.Work = ports.TerminalSurfaceWorkIdle
	}
	if observation.Work == ports.TerminalSurfaceWorkIdle && observation.Composer == ports.TerminalComposerEmpty {
		observation.NativeConversationNotStarted = kimchiInitialFrameVisible(output)
	}
	return observation
}

func kimchiComposerState(state terminalui.ComposerState) ports.TerminalComposerState {
	switch state {
	case terminalui.ComposerEmpty:
		return ports.TerminalComposerEmpty
	case terminalui.ComposerDraft:
		return ports.TerminalComposerDraft
	default:
		return ports.TerminalComposerUnknown
	}
}

// kimchiDecisionFrameVisible recognizes Kimchi's live selection dialog (" ↑↓
// navigate • 1-9 pick • Enter select • Esc cancel" beneath a highlighted option
// row). The footer text is provider-owned and fixed; transcript prose may quote
// it, so the frame only counts while nothing newer renders below it and no
// spinner shows the turn resumed.
func kimchiDecisionFrameVisible(output string) bool {
	lines := kimchiTailLines(output)
	footer := -1
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "Enter select") && strings.Contains(line, "Esc cancel") {
			footer = i
			break
		}
	}
	if footer < 0 {
		return false
	}
	// Nothing newer than the decision may exist below its footer: a composer or
	// a spinner means the dialog was answered and its rows are scrollback.
	for i := footer + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "❯") || kimchiSpinnerRow(line) {
			return false
		}
	}
	// Require the highlighted current option above the footer so the footer
	// alone in quoted prose cannot form a decision frame. Unselected sibling
	// options sit between the highlighted row and the footer.
	for i := footer - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "> ") {
			return true
		}
		if line == "" || kimchiNumberedOption(line) {
			continue
		}
		return false
	}
	return false
}

// kimchiNumberedOption matches one unordered sibling option row ("2. 3").
func kimchiNumberedOption(line string) bool {
	dot := strings.IndexByte(line, '.')
	if dot <= 0 {
		return false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// kimchiActiveFrameVisible recognizes Kimchi's live spinner row ("  \ Chopping..")
// in the captured tail. pi erases the row when the turn settles, so the glyph
// pattern only exists while work is in flight.
func kimchiActiveFrameVisible(output string) bool {
	for _, line := range kimchiTailLines(output) {
		if kimchiSpinnerRow(line) {
			return true
		}
	}
	return false
}

// kimchiSpinnerRow matches one transient spinner row: a cycling glyph, a short
// verb, and one to three progress dots (observed: "- Chopping.", "\ Chopping.").
// Finalized transcript rows (" ▍ Thought for 1s") and status chrome fail the
// glyph or the dot rule.
func kimchiSpinnerRow(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || utf8.RuneCountInString(line) > 80 {
		return false
	}
	first, size := utf8.DecodeRuneInString(line)
	if !strings.ContainsRune(`-\/|`, first) {
		return false
	}
	rest := strings.TrimSpace(line[size:])
	dots := 0
	for strings.HasSuffix(rest, ".") {
		rest = rest[:len(rest)-1]
		dots++
	}
	if rest == "" || dots < 1 || dots > 3 {
		return false
	}
	return !strings.ContainsAny(rest, "•·→─│")
}

// kimchiInitialFrameVisible proves the TUI still shows its startup banner with
// only the current composer and no provider activity: exactly one prompt row,
// the version/workdir banner, and no tool or assistant rows (" ▍ …").
func kimchiInitialFrameVisible(output string) bool {
	header, prompts, activity := false, 0, false
	for _, line := range terminalui.PlainTerminalLines(output) {
		line = strings.TrimSpace(line)
		if kimchiVersionBannerPattern.MatchString(line) {
			header = true
		}
		if strings.HasPrefix(line, "❯") {
			prompts++
		}
		if strings.Contains(line, " ▍") {
			activity = true
		}
	}
	return header && prompts == 1 && !activity
}

func kimchiTailLines(output string) []string {
	lines := terminalui.PlainTerminalLines(output)
	if len(lines) > kimchiTerminalSurfaceLookbackLines {
		lines = lines[len(lines)-kimchiTerminalSurfaceLookbackLines:]
	}
	return lines
}

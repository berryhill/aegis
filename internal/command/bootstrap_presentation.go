package command

import (
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
)

type bootstrapPresentationDepth string

const (
	bootstrapBasic    bootstrapPresentationDepth = "basic"
	bootstrapAdvanced bootstrapPresentationDepth = "advanced"
)

type bootstrapPresentation struct {
	depth bootstrapPresentationDepth
	width int
}

type bootstrapDecision struct {
	Title          string
	Recommendation string
	Consequence    string
	Details        string
	DefaultDecline bool
}

func newBootstrapPresentation(capabilities tui.Capabilities) *bootstrapPresentation {
	width := capabilities.Width
	if width < 40 {
		width = 40
	}
	return &bootstrapPresentation{depth: bootstrapBasic, width: width}
}

func bootstrapView(views []*bootstrapPresentation) *bootstrapPresentation {
	if len(views) != 0 && views[0] != nil {
		return views[0]
	}
	return newBootstrapPresentation(tui.Capabilities{Width: 80})
}

func (presentation *bootstrapPresentation) setDepth(depth bootstrapPresentationDepth) {
	if depth == bootstrapBasic || depth == bootstrapAdvanced {
		presentation.depth = depth
	}
}

func (presentation *bootstrapPresentation) renderIntroduction(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "PRESENTATION / basic (default)")
	presentation.writeBlock(cmd, "GUIDANCE", "One decision is shown at a time. Enter 'details' or 'advanced' before any approval to inspect exact authoritative evidence; enter 'basic' to return to concise guidance. Presentation depth never changes verified progress or authority.")
}

func (presentation *bootstrapPresentation) renderDecision(cmd *cobra.Command, decision bootstrapDecision) bool {
	fmt.Fprintln(cmd.OutOrStdout(), "\nDECISION / "+decision.Title)
	presentation.writeBlock(cmd, "RECOMMENDATION", decision.Recommendation)
	presentation.writeBlock(cmd, "CONSEQUENCE", decision.Consequence)
	if presentation.depth == bootstrapAdvanced {
		presentation.writeBlock(cmd, "DETAILS / authoritative Aegis evidence", decision.Details)
		return true
	}
	return false
}

func (presentation *bootstrapPresentation) approve(cmd *cobra.Command, input *terminalInput, decision bootstrapDecision) (bool, error) {
	detailsShown := presentation.renderDecision(cmd, decision)
	for {
		prompt := "Approve? [Y/n/details/basic/advanced]: "
		if decision.DefaultDecline {
			prompt = "Approve? [y/N/details/basic/advanced]: "
		}
		if presentation.width < 60 {
			prompt = "Approve? [Y/n/details]: "
			if decision.DefaultDecline {
				prompt = "Approve? [y/N/details]: "
			}
		}
		fmt.Fprint(cmd.OutOrStdout(), prompt)
		answer, eof, err := readBootstrapLine(cmd, input, 32)
		if err != nil || eof {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "":
			return !decision.DefaultDecline, nil
		case "y", "yes", "1", "start":
			return true, nil
		case "n", "no", "exit", "cancel", "2":
			return false, nil
		case "details", "advanced", "a":
			fmt.Fprintln(cmd.OutOrStdout())
			presentation.setDepth(bootstrapAdvanced)
			if !detailsShown {
				presentation.writeBlock(cmd, "DETAILS / authoritative Aegis evidence", decision.Details)
				detailsShown = true
			}
		case "basic", "b":
			fmt.Fprintln(cmd.OutOrStdout())
			presentation.setDepth(bootstrapBasic)
			fmt.Fprintln(cmd.OutOrStdout(), "PRESENTATION / basic; verified progress is unchanged")
		default:
			fmt.Fprintln(cmd.OutOrStdout(), "Unrecognized answer; cancelled without mutation.")
			return false, nil
		}
	}
}

// chooseCustody keeps recommended and exact advanced custody routes on the
// shared presenter. An empty result is the safe, non-mutating decline.
func (presentation *bootstrapPresentation) chooseCustody(cmd *cobra.Command, input *terminalInput, decision bootstrapDecision) (string, error) {
	detailsShown := presentation.renderDecision(cmd, decision)
	for {
		prompt := "Choose custody [Y=encrypted/n=exit/advanced]: "
		if presentation.width < 60 {
			prompt = "Custody [Y/n/details]: "
		}
		fmt.Fprint(cmd.OutOrStdout(), prompt)
		answer, eof, err := readBootstrapLine(cmd, input, 32)
		if err != nil || eof {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes", "passphrase-file":
			return "passphrase-file", nil
		case "n", "no", "exit", "cancel", "3":
			return "", nil
		case "details", "advanced", "a":
			fmt.Fprintln(cmd.OutOrStdout())
			presentation.setDepth(bootstrapAdvanced)
			if !detailsShown {
				presentation.writeBlock(cmd, "DETAILS / authoritative Aegis evidence", decision.Details)
				detailsShown = true
			}
			presentation.writeBlock(cmd, "EXACT CHOICES", "[1] passphrase-encrypted local key (recommended)\n[2] systemd service credential (must already be delivered by a service unit)\n[3] plaintext host file (development only; weaker)\n[4] exit without mutation")
			fmt.Fprint(cmd.OutOrStdout(), "Select exact custody [1/2/3/4]: ")
			choice, ended, readErr := readBootstrapLine(cmd, input, 32)
			if readErr != nil || ended {
				return "", readErr
			}
			switch strings.ToLower(strings.TrimSpace(choice)) {
			case "1", "passphrase-file":
				return "passphrase-file", nil
			case "2", "systemd":
				return "systemd", nil
			case "3", "host-file":
				return "host-file", nil
			case "4", "exit", "cancel":
				return "", nil
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "No valid custody choice selected; no mutation performed.")
				return "", nil
			}
		case "basic", "b":
			fmt.Fprintln(cmd.OutOrStdout())
			presentation.setDepth(bootstrapBasic)
			fmt.Fprintln(cmd.OutOrStdout(), "PRESENTATION / basic; verified progress is unchanged")
		default:
			fmt.Fprintln(cmd.OutOrStdout(), "No valid custody choice selected; no mutation performed.")
			return "", nil
		}
	}
}

func (presentation *bootstrapPresentation) writeBlock(cmd *cobra.Command, label, text string) {
	fmt.Fprintln(cmd.OutOrStdout(), label)
	text = tui.Sanitize(text, tui.DefaultSanitizeOptions(tui.Prose))
	for _, paragraph := range strings.Split(text, "\n") {
		for _, line := range wrapBootstrapText(paragraph, max(presentation.width-2, 20)) {
			fmt.Fprintln(cmd.OutOrStdout(), "  "+line)
		}
	}
}

func wrapBootstrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 1)
	line := ""
	for _, word := range words {
		runes := []rune(word)
		for len(runes) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		word = string(runes)
		if word == "" {
			continue
		}
		if line == "" {
			line = word
			continue
		}
		if len([]rune(line))+1+len(runes) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

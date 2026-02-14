package tui

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// terminalTickMsg triggers a re-render of the embedded terminal.
type terminalTickMsg struct{}

// terminalExitMsg signals that the embedded process has exited.
type terminalExitMsg struct{ err error }

// terminalTick returns a command that ticks at ~30fps for terminal re-renders.
func terminalTick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(time.Time) tea.Msg {
		return terminalTickMsg{}
	})
}

// TerminalModel is a Bubble Tea component that runs a command inside an
// embedded virtual terminal (charmbracelet/x/vt) backed by a PTY.
type TerminalModel struct {
	emulator *vt.Emulator
	ptmx     *os.File // PTY master
	cmd      *exec.Cmd
	width    int
	height   int
	focused  bool
	done     bool
	exitErr  error

	mu sync.Mutex // guards done/exitErr
}

// NewTerminal creates a TerminalModel, starts the command in a PTY, and
// returns an initial tick command to begin rendering.
func NewTerminal(w, h int, cmd *exec.Cmd) (*TerminalModel, tea.Cmd) {
	em := vt.NewEmulator(w, h)

	// Start command in a PTY.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	})
	if err != nil {
		// Return a model that is immediately done with an error.
		return &TerminalModel{
			emulator: em,
			width:    w,
			height:   h,
			done:     true,
			exitErr:  err,
		}, func() tea.Msg { return terminalExitMsg{err: err} }
	}

	t := &TerminalModel{
		emulator: em,
		ptmx:     ptmx,
		cmd:      cmd,
		width:    w,
		height:   h,
	}

	// Copy PTY output into the emulator.
	go func() {
		_, _ = io.Copy(em, ptmx)
		// Process exited — mark done.
		waitErr := cmd.Wait()
		t.mu.Lock()
		t.done = true
		t.exitErr = waitErr
		t.mu.Unlock()
	}()

	return t, terminalTick()
}

// Update processes messages for the terminal component.
func (t *TerminalModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.focused && t.ptmx != nil {
			b := encodeKey(msg)
			if len(b) > 0 {
				_, _ = t.ptmx.Write(b)
			}
		}
		return nil

	case terminalTickMsg:
		if t.Done() {
			return func() tea.Msg { return terminalExitMsg{err: t.ExitErr()} }
		}
		return terminalTick()
	}

	return nil
}

// View renders the terminal emulator contents.
func (t *TerminalModel) View() string {
	return t.emulator.Render()
}

// Focus gives input focus to this terminal.
func (t *TerminalModel) Focus() { t.focused = true }

// Blur removes input focus from this terminal.
func (t *TerminalModel) Blur() { t.focused = false }

// Focused returns whether the terminal currently has input focus.
func (t *TerminalModel) Focused() bool { return t.focused }

// Resize resizes the terminal emulator and PTY.
func (t *TerminalModel) Resize(w, h int) {
	t.width = w
	t.height = h
	t.emulator.Resize(w, h)
	if t.ptmx != nil {
		_ = pty.Setsize(t.ptmx, &pty.Winsize{
			Rows: uint16(h),
			Cols: uint16(w),
		})
	}
}

// Close kills the process and cleans up resources.
func (t *TerminalModel) Close() {
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	if t.ptmx != nil {
		_ = t.ptmx.Close()
	}
}

// Done returns whether the subprocess has exited.
func (t *TerminalModel) Done() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

// ExitErr returns the error from the subprocess exit, if any.
func (t *TerminalModel) ExitErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitErr
}

// encodeKey converts a tea.KeyMsg into raw bytes suitable for writing to a PTY.
func encodeKey(msg tea.KeyMsg) []byte {
	// For rune-based keys (regular typing), just return the runes as bytes.
	if len(msg.Runes) > 0 {
		s := string(msg.Runes)
		return []byte(s)
	}

	// Map special keys to their VT100/xterm escape sequences.
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "backspace":
		return []byte{127}
	case "tab":
		return []byte{'\t'}
	case "shift+tab":
		return []byte("\x1b[Z")
	case "space":
		return []byte{' '}
	case "escape", "esc":
		return []byte{0x1b}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	case "insert":
		return []byte("\x1b[2~")
	case "delete":
		return []byte("\x1b[3~")
	case "f1":
		return []byte("\x1bOP")
	case "f2":
		return []byte("\x1bOQ")
	case "f3":
		return []byte("\x1bOR")
	case "f4":
		return []byte("\x1bOS")
	case "f5":
		return []byte("\x1b[15~")
	case "f6":
		return []byte("\x1b[17~")
	case "f7":
		return []byte("\x1b[18~")
	case "f8":
		return []byte("\x1b[19~")
	case "f9":
		return []byte("\x1b[20~")
	case "f10":
		return []byte("\x1b[21~")
	case "f11":
		return []byte("\x1b[23~")
	case "f12":
		return []byte("\x1b[24~")

	// Ctrl key combinations
	case "ctrl+a":
		return []byte{0x01}
	case "ctrl+b":
		return []byte{0x02}
	case "ctrl+c":
		return []byte{0x03}
	case "ctrl+d":
		return []byte{0x04}
	case "ctrl+e":
		return []byte{0x05}
	case "ctrl+f":
		return []byte{0x06}
	case "ctrl+g":
		return []byte{0x07}
	case "ctrl+h":
		return []byte{0x08}
	case "ctrl+i":
		return []byte{0x09}
	case "ctrl+j":
		return []byte{0x0a}
	case "ctrl+k":
		return []byte{0x0b}
	case "ctrl+l":
		return []byte{0x0c}
	case "ctrl+m":
		return []byte{0x0d}
	case "ctrl+n":
		return []byte{0x0e}
	case "ctrl+o":
		return []byte{0x0f}
	case "ctrl+p":
		return []byte{0x10}
	case "ctrl+q":
		return []byte{0x11}
	case "ctrl+r":
		return []byte{0x12}
	case "ctrl+s":
		return []byte{0x13}
	case "ctrl+t":
		return []byte{0x14}
	case "ctrl+u":
		return []byte{0x15}
	case "ctrl+v":
		return []byte{0x16}
	case "ctrl+w":
		return []byte{0x17}
	case "ctrl+x":
		return []byte{0x18}
	case "ctrl+y":
		return []byte{0x19}
	case "ctrl+z":
		return []byte{0x1a}
	}

	return nil
}

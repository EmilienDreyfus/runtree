package termui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	defaultFrameInterval = 80 * time.Millisecond
	clearLine            = "\r\033[2K"
)

var defaultFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Step interface {
	Success(message string)
	Fail(message string)
	Skip(message string)
	Stop()
}

type Reporter interface {
	Start(message string) Step
}

type Options struct {
	Interactive   *bool
	Color         *bool
	FrameInterval time.Duration
}

type Renderer struct {
	out           io.Writer
	interactive   bool
	color         bool
	frameInterval time.Duration
	frames        []string
}

func New(out io.Writer) *Renderer {
	return NewWithOptions(out, Options{})
}

func NewWithOptions(out io.Writer, opts Options) *Renderer {
	if out == nil {
		out = io.Discard
	}

	interactive := detectInteractive(out)
	if opts.Interactive != nil {
		interactive = *opts.Interactive
	}

	color := interactive && os.Getenv("NO_COLOR") == ""
	if opts.Color != nil {
		color = *opts.Color
	}

	frameInterval := opts.FrameInterval
	if frameInterval <= 0 {
		frameInterval = defaultFrameInterval
	}

	return &Renderer{
		out:           out,
		interactive:   interactive,
		color:         color,
		frameInterval: frameInterval,
		frames:        defaultFrames,
	}
}

func (r *Renderer) Start(message string) Step {
	message = normalizeMessage(message)
	step := &spinnerStep{
		renderer: r,
		message:  message,
	}

	if !r.interactive {
		fmt.Fprintln(r.out, message)
		return step
	}

	step.done = make(chan struct{})
	step.stopped = make(chan struct{})
	step.renderFrame(0)
	go step.animate()
	return step
}

func detectInteractive(out io.Writer) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func normalizeMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Working"
	}
	return message
}

type spinnerStep struct {
	renderer *Renderer
	message  string
	done     chan struct{}
	stopped  chan struct{}
	once     sync.Once
	mu       sync.Mutex
}

func (s *spinnerStep) Success(message string) {
	s.finish("✓", "green", message)
}

func (s *spinnerStep) Fail(message string) {
	s.finish("✕", "red", message)
}

func (s *spinnerStep) Skip(message string) {
	s.finish("-", "yellow", message)
}

func (s *spinnerStep) Stop() {
	s.once.Do(func() {
		s.stopAnimation()
		if s.renderer.interactive {
			s.mu.Lock()
			defer s.mu.Unlock()
			fmt.Fprint(s.renderer.out, clearLine)
		}
	})
}

func (s *spinnerStep) animate() {
	defer close(s.stopped)
	ticker := time.NewTicker(s.renderer.frameInterval)
	defer ticker.Stop()

	frame := 1
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.renderFrame(frame)
			frame++
		}
	}
}

func (s *spinnerStep) renderFrame(index int) {
	frame := s.renderer.frames[index%len(s.renderer.frames)]
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.renderer.out, "%s%s %s", clearLine, s.renderer.styled(frame, "cyan"), s.message)
}

func (s *spinnerStep) finish(symbol, color, message string) {
	message = normalizeMessage(message)
	s.once.Do(func() {
		s.stopAnimation()
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.renderer.interactive {
			fmt.Fprint(s.renderer.out, clearLine)
		}
		fmt.Fprintf(s.renderer.out, "%s %s\n", s.renderer.styled(symbol, color), message)
	})
}

func (s *spinnerStep) stopAnimation() {
	if s.done == nil {
		return
	}
	close(s.done)
	<-s.stopped
}

func (r *Renderer) styled(value, color string) string {
	if !r.color {
		return value
	}

	switch color {
	case "cyan":
		return "\033[36m" + value + "\033[0m"
	case "green":
		return "\033[32m" + value + "\033[0m"
	case "red":
		return "\033[31m" + value + "\033[0m"
	case "yellow":
		return "\033[33m" + value + "\033[0m"
	default:
		return value
	}
}

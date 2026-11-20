package release

import (
	"fmt"
	"io"
	"strings"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/output"
)

// progress reports transfer progress on stderr when it is a terminal and
// stays silent otherwise. A total below zero means the size is unknown.
type progress struct {
	r              io.Reader
	w              io.Writer
	enabled        bool
	verb, pastVerb string
	name           string
	done, total    int64
	last           int64
}

func newProgress(f *app.Factory, r io.Reader, verb, pastVerb, name string, total int64) *progress {
	p := &progress{
		r: r, w: f.IO.ErrOut, enabled: f.IO.IsStderrTTY(), verb: verb, pastVerb: pastVerb,
		name: output.Sanitize(name), total: total, last: -1,
	}
	p.report()
	return p
}

// reader returns the reader to transfer from: r itself without a terminal.
func (p *progress) reader() io.Reader {
	if !p.enabled {
		return p.r
	}
	return p
}

func (p *progress) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report()
	return n, err
}

// step changes once per percent with a known size and once per MiB without.
func (p *progress) step() int64 {
	switch {
	case p.total < 0:
		return p.done >> 20
	case p.total == 0:
		return 100
	default:
		return p.done * 100 / p.total
	}
}

func (p *progress) report() {
	step := p.step()
	if !p.enabled || step == p.last {
		return
	}
	p.last = step
	if p.total < 0 {
		fmt.Fprintf(p.w, "\r%s %s (%s)", p.verb, p.name, formatBytes(p.done))
		return
	}
	fmt.Fprintf(p.w, "\r%s %s %3d%% (%s / %s)", p.verb, p.name, step, formatBytes(p.done), formatBytes(p.total))
}

// finish ends the progress line; note is appended to the success message.
func (p *progress) finish(ok bool, note string) {
	if !p.enabled {
		return
	}
	if !ok {
		fmt.Fprintln(p.w)
		return
	}
	size := p.total
	if size < 0 {
		size = p.done
	}
	fmt.Fprintf(p.w, "\r%s %s (%s%s)%s\n", p.pastVerb, p.name, formatBytes(size), note, strings.Repeat(" ", 24))
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

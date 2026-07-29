package runner

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"
)

// TextReporter prints progress and one verdict line per result, plus a final
// tally. When the writer is a terminal, the in-progress "RUN" line is
// overwritten in place by the verdict; when piped/redirected, each phase gets
// its own line so the output stays readable in logs.
//
// If Out is set, a clean (always non-TTY) copy of the same output is mirrored
// there, e.g. for a report file while still showing live progress on stdout.
type TextReporter struct {
	W    io.Writer
	Out  io.Writer // optional second sink; always treated as non-TTY
	tty  bool      // set on first use
	done bool      // whether tty has been probed
}

func (t *TextReporter) isTTY() bool {
	if !t.done {
		t.done = true
		if f, ok := t.W.(*os.File); ok {
			fi, err := f.Stat()
			if err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
				t.tty = true
			}
		}
	}
	return t.tty
}

// startPrefix is the label printed before a case runs, padded to match the
// width of the PASS/FAIL/SKIP verdict prefixes so columns line up.
const startPrefix = "RUN  "

func (t *TextReporter) Start(name, desc string) {
	if t.isTTY() {
		// No trailing newline: the verdict in Result() overwrites this line.
		fmt.Fprintf(t.W, "%s%s ... ", startPrefix, name)
	} else {
		// Non-TTY (piped/logged): print on its own line so nothing is lost.
		fmt.Fprintf(t.W, "%s%s ... %s\n", startPrefix, name, desc)
	}
	if t.Out != nil {
		fmt.Fprintf(t.Out, "%s%s ... %s\n", startPrefix, name, desc)
	}
}

func (t *TextReporter) Result(r *Result) {
	verdict := FormatVerdict(r)
	if t.isTTY() {
		// Overwrite the in-progress "RUN" line.
		fmt.Fprintf(t.W, "\r%s", verdict)
		// Pad to clear any leftover characters from the longer "RUN" line.
		runLineLen := len(startPrefix) + len(r.Name) + len(" ... ")
		if pad := runLineLen - len(verdict); pad > 0 {
			fmt.Fprint(t.W, strings.Repeat(" ", pad))
		}
		fmt.Fprintln(t.W)
	} else {
		// Non-TTY: print the verdict on its own line, indented under the RUN line.
		fmt.Fprintf(t.W, "  -> %s\n", verdict)
	}
	if t.Out != nil {
		fmt.Fprintf(t.Out, "  -> %s\n", verdict)
	}
}

func (t *TextReporter) Summary(w io.Writer, passed, total int, duration time.Duration) {
	status := "PASS"
	if passed != total {
		status = "FAIL"
	}
	line := fmt.Sprintf("\n%s  %d/%d cases passed  (%s)\n", status, passed, total, fmtDur(duration, "total"))
	fmt.Fprint(w, line)
	if t.Out != nil {
		fmt.Fprint(t.Out, line)
	}
}

// List renders the set of available cases (for the `list` command).
var listTmpl = template.Must(template.New("cases").Parse(
	`{{range .}}{{.Name}}	{{.Desc}}
{{end}}`))

func List(w io.Writer, cases []Case) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	type row struct{ Name, Desc string }
	var rows []row
	for _, c := range cases {
		rows = append(rows, row{c.Name(), c.Desc()})
	}
	listTmpl.Execute(tw, rows)
	tw.Flush()
}

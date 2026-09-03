// Package parser handles container log line parsing: detecting and
// normalizing both container-runtime log line formats (containerd/CRI-O's
// plaintext CRI format, and Docker's older json-file format), reassembling
// runtime-split partial lines, and extracting app-level trace correlation
// data from JSON application logs.
package parser

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CRILine is one normalized container-runtime log record, after any
// partial-line reassembly.
type CRILine struct {
	Timestamp time.Time
	Stream    string // "stdout" | "stderr"
	Content   string // the application's log line, runtime framing stripped
}

// rawLine is an intermediate, pre-reassembly parse result.
type rawLine struct {
	timestamp time.Time
	stream    string
	partial   bool
	content   string
}

// dockerJSONLine mirrors Docker's json-file log driver record shape.
type dockerJSONLine struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

// parseLine detects and parses a single raw line from a container log file
// in either the plaintext CRI format
// ("<RFC3339Nano-ts> <stream> <F|P> <content>") written by containerd/CRI-O,
// or the JSON format ({"log":...,"stream":...,"time":...}) written by
// Docker's json-file driver.
func parseLine(line string) (rawLine, error) {
	line = strings.TrimRight(line, "\n")
	if line == "" {
		return rawLine{}, fmt.Errorf("empty line")
	}
	if line[0] == '{' {
		return parseDockerJSONLine(line)
	}
	return parseCRIPlaintextLine(line)
}

func parseDockerJSONLine(line string) (rawLine, error) {
	var d dockerJSONLine
	if err := json.Unmarshal([]byte(line), &d); err != nil {
		return rawLine{}, fmt.Errorf("parse docker json line: %w", err)
	}
	ts, err := time.Parse(time.RFC3339Nano, d.Time)
	if err != nil {
		return rawLine{}, fmt.Errorf("parse docker json timestamp: %w", err)
	}
	return rawLine{
		timestamp: ts,
		stream:    d.Stream,
		partial:   false, // the docker json-file driver does not split lines
		content:   strings.TrimSuffix(d.Log, "\n"),
	}, nil
}

func parseCRIPlaintextLine(line string) (rawLine, error) {
	// "<timestamp> <stream> <F|P> <content...>" — SplitN(4) because content
	// itself may legitimately contain spaces.
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return rawLine{}, fmt.Errorf("malformed CRI plaintext line: %q", line)
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return rawLine{}, fmt.Errorf("parse CRI timestamp: %w", err)
	}
	var partial bool
	switch parts[2] {
	case "F":
		partial = false
	case "P":
		partial = true
	default:
		return rawLine{}, fmt.Errorf("unrecognized CRI tag %q", parts[2])
	}
	return rawLine{
		timestamp: ts,
		stream:    parts[1],
		partial:   partial,
		content:   parts[3],
	}, nil
}

// Reassembler stitches together runtime-split partial ("P"-tagged) lines
// into one logical log line before it is treated as complete. Each tracked
// file needs its own Reassembler instance — partial-line state is per-file.
type Reassembler struct {
	pending strings.Builder
	first   rawLine
	active  bool
}

// Feed parses one raw line and returns a complete CRILine plus true when a
// full logical line is ready. A "P"-tagged (partial) line is buffered and
// never itself ready; the "F"-tagged (final) line that follows completes
// the sequence, using the first segment's timestamp/stream and the
// concatenation of all segments' content. A standalone "F" line with no
// preceding "P" segments — the common case — is ready immediately.
// Malformed lines are returned as an error; callers should skip them and
// continue rather than aborting the tail.
func (r *Reassembler) Feed(raw string) (CRILine, bool, error) {
	pl, err := parseLine(raw)
	if err != nil {
		return CRILine{}, false, err
	}

	if pl.partial {
		if !r.active {
			r.active = true
			r.first = pl
		}
		r.pending.WriteString(pl.content)
		return CRILine{}, false, nil
	}

	// Non-partial (F) line.
	if !r.active {
		return CRILine{Timestamp: pl.timestamp, Stream: pl.stream, Content: pl.content}, true, nil
	}
	r.pending.WriteString(pl.content)
	return r.flush(), true, nil
}

func (r *Reassembler) flush() CRILine {
	line := CRILine{Timestamp: r.first.timestamp, Stream: r.first.stream, Content: r.pending.String()}
	r.pending.Reset()
	r.active = false
	return line
}

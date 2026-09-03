package parser

import (
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		wantErr    bool
		wantStream string
		wantPart   bool
		wantBody   string
	}{
		{
			name:       "cri plaintext full line",
			line:       `2026-09-03T00:17:09.669794202Z stdout F {"msg":"hello","trace_id":"abc"}`,
			wantStream: "stdout",
			wantPart:   false,
			wantBody:   `{"msg":"hello","trace_id":"abc"}`,
		},
		{
			name:       "cri plaintext partial line",
			line:       `2026-09-03T00:17:09.669794202Z stdout P This is a partial segment`,
			wantStream: "stdout",
			wantPart:   true,
			wantBody:   "This is a partial segment",
		},
		{
			name:       "cri plaintext stderr",
			line:       `2026-09-03T00:17:09.669794202Z stderr F panic: boom`,
			wantStream: "stderr",
			wantPart:   false,
			wantBody:   "panic: boom",
		},
		{
			name:       "docker json-file format",
			line:       `{"log":"hello world\n","stream":"stdout","time":"2026-09-03T00:17:09.669794202Z"}`,
			wantStream: "stdout",
			wantPart:   false,
			wantBody:   "hello world",
		},
		{
			name:    "malformed plaintext - too few fields",
			line:    `2026-09-03T00:17:09.669794202Z stdout`,
			wantErr: true,
		},
		{
			name:    "malformed plaintext - bad tag",
			line:    `2026-09-03T00:17:09.669794202Z stdout X content`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			line:    `{"log": not valid json`,
			wantErr: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pl, err := parseLine(tc.line)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (result: %+v)", pl)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pl.stream != tc.wantStream {
				t.Errorf("stream: got %q want %q", pl.stream, tc.wantStream)
			}
			if pl.partial != tc.wantPart {
				t.Errorf("partial: got %v want %v", pl.partial, tc.wantPart)
			}
			if pl.content != tc.wantBody {
				t.Errorf("content: got %q want %q", pl.content, tc.wantBody)
			}
		})
	}
}

func TestReassemblerSingleFullLine(t *testing.T) {
	var r Reassembler
	line, ready, err := r.Feed(`2026-09-03T00:17:09.000000000Z stdout F {"msg":"hi"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true for a standalone F line")
	}
	if line.Content != `{"msg":"hi"}` {
		t.Errorf("unexpected content: %q", line.Content)
	}
}

func TestReassemblerSplitLine(t *testing.T) {
	var r Reassembler

	_, ready, err := r.Feed(`2026-09-03T00:17:09.000000000Z stdout P {"msg":"hel`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false after first partial segment")
	}

	_, ready, err = r.Feed(`2026-09-03T00:17:09.100000000Z stdout P lo wor`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false after second partial segment")
	}

	line, ready, err := r.Feed(`2026-09-03T00:17:09.200000000Z stdout F ld"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true after final segment")
	}
	want := `{"msg":"hello world"}`
	if line.Content != want {
		t.Errorf("reassembled content: got %q want %q", line.Content, want)
	}
	// Timestamp/stream should come from the first segment.
	if line.Stream != "stdout" {
		t.Errorf("unexpected stream: %q", line.Stream)
	}
}

func TestReassemblerResetsAfterFlush(t *testing.T) {
	var r Reassembler
	r.Feed(`2026-09-03T00:17:09.000000000Z stdout P partial-`)
	r.Feed(`2026-09-03T00:17:09.100000000Z stdout F one`)

	// A fresh standalone line after a completed sequence must not be
	// affected by prior state.
	line, ready, err := r.Feed(`2026-09-03T00:17:10.000000000Z stdout F unrelated line`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready || line.Content != "unrelated line" {
		t.Errorf("got %+v ready=%v, want a clean standalone line", line, ready)
	}
}

func TestExtractTraceID(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		field       string
		wantIsJSON  bool
		wantTraceID string
	}{
		{"string field", `{"trace_id":"abc-123","msg":"hi"}`, "trace_id", true, "abc-123"},
		{"numeric field", `{"trace_id":12345,"msg":"hi"}`, "trace_id", true, "12345"},
		{"custom field name", `{"correlation_id":"xyz","msg":"hi"}`, "correlation_id", true, "xyz"},
		{"missing field", `{"msg":"hi"}`, "trace_id", true, ""},
		{"not json", `plain text log line`, "trace_id", false, ""},
		{"wrong type field", `{"trace_id":{"nested":true}}`, "trace_id", true, ""},
		{"empty content", ``, "trace_id", false, ""},
		{"malformed json", `{"trace_id": `, "trace_id", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isJSON, traceID := ExtractTraceID(tc.content, tc.field)
			if isJSON != tc.wantIsJSON {
				t.Errorf("isJSON: got %v want %v", isJSON, tc.wantIsJSON)
			}
			if traceID != tc.wantTraceID {
				t.Errorf("traceID: got %q want %q", traceID, tc.wantTraceID)
			}
		})
	}
}

func TestParsePodLogPath(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		want    PathMeta
		wantErr bool
	}{
		{
			name: "standard path",
			path: "/var/log/pods/default_app-abc123_9f2e4b3a-1111-2222-3333-444455556666/app/0.log",
			want: PathMeta{Namespace: "default", Pod: "app-abc123", PodUID: "9f2e4b3a-1111-2222-3333-444455556666", Container: "app"},
		},
		{
			name: "kube-system namespace",
			path: "/var/log/pods/kube-system_coredns-abc_uid1/coredns/3.log",
			want: PathMeta{Namespace: "kube-system", Pod: "coredns-abc", PodUID: "uid1", Container: "coredns"},
		},
		{
			name:    "malformed pod dir",
			path:    "/var/log/pods/not-enough-parts/app/0.log",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePodLogPath(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

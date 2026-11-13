// Package pii scans tracked blobs for the baseline cloud identifier shapes.
// This stdlib-only source also produces the standalone CI helper.
package pii

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	AllowlistPath = ".github/pii-allowlist.txt"
	WorkflowPath  = ".github/workflows/pii-gate.yml"
	HelperPath    = ".github/scripts/pii-gate.go"
	CheckName     = "pii-gate"
)

var shapes = []struct {
	name, pattern string
}{
	{"account-id", `[0-9]{12}`},
	{"ami", `ami-0[0-9a-f]{16}`},
	{"instance", `i-0[0-9a-f]{8,17}`},
	{"security-group", `sg-0[0-9a-f]{8,17}`},
	{"subnet", `subnet-0[0-9a-f]{8,17}`},
	{"vpc", `vpc-0[0-9a-f]{8,17}`},
	{"volume", `vol-0[0-9a-f]{8,17}`},
	{"snapshot", `snap-0[0-9a-f]{8,17}`},
}

var synthetic = map[string]bool{
	"123456789012": true, "000000000000": true, "999999999999": true,
	"ami-0123456789abcdef0": true, "ami-0abcdef1234567890": true,
}

var identifierRE = func() *regexp.Regexp {
	patterns := make([]string, len(shapes))
	for i, shape := range shapes {
		patterns[i] = "(" + shape.pattern + ")"
	}
	return regexp.MustCompile(strings.Join(patterns, "|"))
}()

// Match values are available only for local bootstrap output, never diagnostics.
type Match struct {
	Value, Shape string
	Line         int
}

type Policy map[string]bool

func word(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func recognized(data []byte) []Match {
	var matches []Match
	line, previous := 1, 0
	for _, indexes := range identifierRE.FindAllSubmatchIndex(data, -1) {
		start, end := indexes[0], indexes[1]
		if start > 0 && word(data[start-1]) || end < len(data) && word(data[end]) {
			continue
		}
		line += bytes.Count(data[previous:start], []byte{'\n'})
		previous = start
		for i, shape := range shapes {
			if indexes[2+i*2] >= 0 {
				matches = append(matches, Match{string(data[start:end]), shape.name, line})
				break
			}
		}
	}
	return matches
}

// SafePath escapes controls and redacts identifier-shaped text in locations.
func SafePath(path string) string {
	return strconv.Quote(identifierRE.ReplaceAllString(path, "[redacted]"))
}

// ParsePolicy accepts only exact complete values; malformed policy fails closed.
func ParsePolicy(data []byte, path string) (Policy, error) {
	policy := Policy{}
	for n, line := range strings.Split(string(data), "\n") {
		value, _, _ := strings.Cut(line, "#")
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		matches := recognized([]byte(value))
		if len(matches) != 1 || matches[0].Value != value {
			return nil, fmt.Errorf("%s:%d: invalid policy; use one exact identifier per line", SafePath(path), n+1)
		}
		policy[value] = true
	}
	return policy, nil
}

// Scan reads all bytes, including binary contents and arbitrarily long lines.
func Scan(r io.Reader, policy Policy) ([]Match, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("cannot read complete blob")
	}
	return ScanBytes(data, policy), nil
}

// ScanBytes applies the same policy to an already-read Git blob.
func ScanBytes(data []byte, policy Policy) []Match {
	var matches []Match
	for _, match := range recognized(data) {
		if !synthetic[match.Value] && !policy[match.Value] {
			matches = append(matches, match)
		}
	}
	return matches
}

func WriteMatches(out io.Writer, path string, matches []Match) {
	for _, match := range matches {
		_, _ = fmt.Fprintf(out, "%s:%d: %s\n", SafePath(path), match.Line, match.Shape)
	}
}

type treeEntry struct {
	path, sha string
}

var objectRE = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func parseManifest(r io.Reader) ([]treeEntry, error) {
	data, err := io.ReadAll(r)
	if err != nil || len(data) > 0 && data[len(data)-1] != 0 {
		return nil, fmt.Errorf("incomplete tracked-tree manifest")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []treeEntry
	seen := map[string]bool{}
	for _, record := range bytes.Split(data[:len(data)-1], []byte{0}) {
		if len(record) == 0 {
			return nil, fmt.Errorf("empty tracked-tree entry")
		}
		header, path, ok := strings.Cut(string(record), "\t")
		fields := strings.Fields(header)
		if !ok || path == "" || seen[path] || len(fields) != 3 || !objectRE.MatchString(fields[2]) {
			return nil, fmt.Errorf("invalid tracked-tree manifest")
		}
		seen[path] = true
		if fields[0] == "160000" && fields[1] == "commit" {
			continue // gitlinks reference another repository, not a tracked blob
		}
		if fields[1] != "blob" || fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
			return nil, fmt.Errorf("invalid tracked blob type")
		}
		entries = append(entries, treeEntry{path, fields[2]})
	}
	return entries, nil
}

func walkBatch(batch io.Reader, entries []treeEntry, visit func(string, []byte) error) error {
	reader := bufio.NewReader(batch)
	for _, entry := range entries {
		header, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("incomplete blob header")
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != entry.sha || fields[1] != "blob" {
			return fmt.Errorf("blob identity or type mismatch")
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("invalid blob size")
		}
		body, err := io.ReadAll(io.LimitReader(reader, size))
		if err != nil || int64(len(body)) != size {
			return fmt.Errorf("incomplete blob contents")
		}
		end, err := reader.ReadByte()
		if err != nil || end != '\n' {
			return fmt.Errorf("invalid blob terminator")
		}
		if err := visit(entry.path, body); err != nil {
			return err
		}
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return fmt.Errorf("unexpected trailing blob data")
	}
	return nil
}

// Run scans ls-tree -rz output paired with cat-file --batch output.
// It returns 0 for clean, 1 for matches, and 2 for unusable/incomplete input.
func Run(manifest io.Reader, batch io.ReadSeeker, out io.Writer) int {
	entries, err := parseManifest(manifest)
	if err != nil {
		_, _ = fmt.Fprintln(out, err)
		return 2
	}
	policy := Policy{}
	err = walkBatch(batch, entries, func(path string, body []byte) error {
		if path == AllowlistPath {
			var err error
			policy, err = ParsePolicy(body, path)
			return err
		}
		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintln(out, err)
		return 2
	}
	if _, err := batch.Seek(0, io.SeekStart); err != nil {
		_, _ = fmt.Fprintln(out, "cannot rewind tracked blobs")
		return 2
	}
	count := 0
	err = walkBatch(batch, entries, func(path string, body []byte) error {
		matches := ScanBytes(body, policy)
		count += len(matches)
		WriteMatches(out, path, matches)
		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintln(out, err)
		return 2
	}
	if count > 0 {
		return 1
	}
	_, _ = fmt.Fprintln(out, "pii-gate: tracked tree clean")
	return 0
}

func RunCLI(args []string, out io.Writer) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(out, "usage: pii-gate MANIFEST BATCH")
		return 2
	}
	manifest, err := os.Open(args[0])
	if err != nil {
		_, _ = fmt.Fprintln(out, "cannot open tracked-tree manifest")
		return 2
	}
	defer func() { _ = manifest.Close() }()
	batch, err := os.Open(args[1])
	if err != nil {
		_, _ = fmt.Fprintln(out, "cannot open tracked blobs")
		return 2
	}
	defer func() { _ = batch.Close() }()
	return Run(manifest, batch, out)
}

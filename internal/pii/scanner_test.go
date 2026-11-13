package pii

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture() string { return "42" + "4242424242" }

func TestScan(t *testing.T) {
	for _, shape := range shapes {
		value := fixture()
		if shape.name != "account-id" {
			prefix := strings.Split(shape.pattern, "0[")[0]
			n := 9
			if shape.name == "ami" {
				n = 16
			}
			value = prefix + "0" + strings.Repeat("b", n)
		}
		t.Run(shape.name, func(t *testing.T) {
			input := "\x00" + strings.Repeat("x", 70000) + " " + value + "\n" + value
			matches, err := Scan(strings.NewReader(input), nil)
			if err != nil || len(matches) != 2 || matches[0].Shape != shape.name || matches[1].Line != 2 {
				t.Fatal("expected two located matches", err)
			}
			for _, s := range []string{"x" + value, value + strings.Repeat("f", 18), value + "_"} {
				got, err := Scan(strings.NewReader(s), nil)
				if err != nil || len(got) != 0 {
					t.Fatal("token boundary", err)
				}
			}
			policy, err := ParsePolicy([]byte(value+"\r\n"+value+" # reviewed\n"), AllowlistPath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Scan(strings.NewReader(input), policy)
			if err != nil || len(got) != 0 {
				t.Fatal("exact exemption", err)
			}
		})
	}
	matches, err := Scan(strings.NewReader("arn:aws:iam::"+fixture()+":root"), nil)
	if err != nil || len(matches) != 1 {
		t.Fatal("ARN account segment", err)
	}
	for value := range synthetic {
		matches, err := Scan(strings.NewReader(value), nil)
		if err != nil || len(matches) != 0 {
			t.Fatal("synthetic exemption", err)
		}
	}
	if _, err := Scan(failingReader{}, nil); err == nil {
		t.Fatal("read failure must fail closed")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("private input") }

func TestPolicyAndDiagnostics(t *testing.T) {
	for _, input := range []string{fixture()[:11], fixture() + "0", fixture() + "*", "^" + fixture() + "$", fixture() + " " + fixture()} {
		_, err := ParsePolicy([]byte("# header\n"+input), AllowlistPath)
		if err == nil || strings.Contains(err.Error(), fixture()) || !strings.Contains(err.Error(), ":2") {
			t.Fatal("invalid policy must have safe line diagnostic")
		}
	}
	policy, err := ParsePolicy([]byte("# "+fixture()+"\n"), AllowlistPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Scan(strings.NewReader("# "+fixture()), policy)
	if err != nil || len(got) != 1 {
		t.Fatal("comments remain scanned", err)
	}
	var out bytes.Buffer
	WriteMatches(&out, fixture()+"\nfile", []Match{{Value: fixture(), Shape: "account-id", Line: 3}})
	if strings.Contains(out.String(), fixture()) || strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), ":3: account-id") {
		t.Fatal("unsafe location diagnostic")
	}
}

func batchFixture(path, content string) ([]byte, []byte) {
	sha := strings.Repeat("a", 40)
	return []byte("100644 blob " + sha + "\t" + path + "\x00"),
		[]byte(fmt.Sprintf("%s blob %d\n%s\n", sha, len(content), content))
}

func TestRun(t *testing.T) {
	for _, tt := range []struct {
		name, path, content string
		want                int
	}{
		{"bad", "file with spaces", fixture(), 1},
		{"synthetic", "file", "123456789012", 0},
		{"empty policy", AllowlistPath, "# reviewed\n", 0},
		{"policy", AllowlistPath, fixture() + "\n", 0},
		{"invalid policy", AllowlistPath, fixture() + "*", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manifest, batch := batchFixture(tt.path, tt.content)
			var out bytes.Buffer
			got := Run(bytes.NewReader(manifest), bytes.NewReader(batch), &out)
			if got != tt.want || strings.Contains(out.String(), fixture()) {
				t.Fatalf("exit %d, expected %d; output must be safe", got, tt.want)
			}
		})
	}
	if Run(strings.NewReader(""), strings.NewReader(""), io.Discard) != 0 {
		t.Fatal("empty tree")
	}
	manifest, batch := batchFixture("file", fixture())
	for _, broken := range [][]byte{batch[:len(batch)-2], append(batch, 'x'), bytes.Replace(batch, []byte("blob"), []byte("tree"), 1), []byte("missing\n")} {
		if Run(bytes.NewReader(manifest), bytes.NewReader(broken), io.Discard) != 2 {
			t.Fatal("incomplete input must fail")
		}
	}
	if Run(strings.NewReader("invalid\x00"), bytes.NewReader(batch), io.Discard) != 2 {
		t.Fatal("invalid manifest")
	}
	manifest = bytes.Replace(manifest, []byte("100644"), []byte("120000"), 1)
	if Run(bytes.NewReader(manifest), bytes.NewReader(batch), io.Discard) != 1 {
		t.Fatal("symlink blob contents must be scanned")
	}
	manifest = []byte("160000 commit " + strings.Repeat("b", 40) + "\tsubmodule\x00")
	if Run(bytes.NewReader(manifest), strings.NewReader(""), io.Discard) != 0 {
		t.Fatal("gitlinks are excluded")
	}
}

func TestLengths(t *testing.T) {
	for _, shape := range shapes[1:] {
		prefix := strings.Split(shape.pattern, "0[")[0]
		for _, n := range []int{7, 8, 16, 17, 18} {
			value := prefix + "0" + strings.Repeat("c", n)
			got, err := Scan(strings.NewReader(value), nil)
			valid := n >= 8 && n <= 17
			if shape.name == "ami" {
				valid = n == 16
			}
			if err != nil || (len(got) == 1) != valid {
				t.Fatal("incorrect shape length boundary", shape.name, n)
			}
		}
	}
}

type badSeeker struct{ *strings.Reader }

func (badSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("private seek error") }

func TestIncompleteInputs(t *testing.T) {
	manifest, batch := batchFixture("file", "safe")
	sha := strings.Repeat("a", 40)
	for _, data := range []string{
		"\x00",
		sha + " blob -1\n",
		sha + " blob nope\n",
		sha + " blob 9\nsafe\n",
		sha + " blob 4\nsafex",
		strings.Repeat("b", 40) + " blob 4\nsafe\n",
	} {
		if Run(bytes.NewReader(manifest), strings.NewReader(data), io.Discard) != 2 {
			t.Fatal("malformed batch must fail")
		}
	}
	for _, data := range []string{
		string(manifest[:len(manifest)-1]),
		string(manifest) + string(manifest),
		"100644 blob invalid\tfile\x00",
		"100644 tree " + sha + "\tfile\x00",
		"100640 blob " + sha + "\tfile\x00",
	} {
		if Run(strings.NewReader(data), bytes.NewReader(batch), io.Discard) != 2 {
			t.Fatal("malformed manifest must fail")
		}
	}
	if Run(failingReader{}, bytes.NewReader(batch), io.Discard) != 2 ||
		Run(bytes.NewReader(manifest), badSeeker{strings.NewReader(string(batch))}, io.Discard) != 2 {
		t.Fatal("reader/seek errors must fail closed")
	}
}

type changingBatch struct{ *strings.Reader }

func (r changingBatch) Seek(int64, int) (int64, error) {
	r.Reset("truncated")
	return 0, nil
}

func TestBatchRereadFailure(t *testing.T) {
	manifest, batch := batchFixture("file", "safe")
	if Run(bytes.NewReader(manifest), changingBatch{strings.NewReader(string(batch))}, io.Discard) != 2 {
		t.Fatal("failure on second pass must not report clean")
	}
}

func TestCLI(t *testing.T) {
	tmp := t.TempDir()
	manifest, batch := batchFixture("file", "safe")
	a, b := filepath.Join(tmp, "manifest"), filepath.Join(tmp, "batch")
	if err := os.WriteFile(a, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, batch, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"missing", b}, {a, "missing"}} {
		if RunCLI(args, io.Discard) != 2 {
			t.Fatal("invalid CLI input must fail")
		}
	}
	if RunCLI([]string{a, b}, io.Discard) != 0 {
		t.Fatal("CLI valid input must pass")
	}
}

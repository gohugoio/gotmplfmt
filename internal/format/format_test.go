package format

import (
	"flag"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// To update the golden files, set writeOutput to true and run `go test -update`.
var (
	update = flag.Bool("update", false, "update the golden files")
)

func TestGolden(t *testing.T) {
	if *update {
		t.Log("Updating golden files...")
	}

	goldenDir := "golden"
	goldenDirIn := filepath.Join(goldenDir, "in")
	goldenDirOut := filepath.Join(goldenDir, "out")

	if *update {
		// Remove existing golden files.
		if err := os.RemoveAll(goldenDirOut); err != nil {
			t.Fatalf("failed to remove existing golden output directory: %v", err)
		}
		if err := os.MkdirAll(goldenDirOut, 0o755); err != nil {
			t.Fatalf("failed to create golden output directory: %v", err)
		}
	}

	// Read golden/in.
	if err := filepath.Walk(goldenDirIn, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		baseName := strings.TrimPrefix(path, goldenDirIn+string(os.PathSeparator))
		t.Run(path, func(t *testing.T) {
			testFormat := func(input string) {
				// We may add some invalid test cases later, but for now assume all input files are valid Go text templates and try parsing them before formatting.
				tryParseGoTextTemplate(t, input)

				output, err := Format(input)
				if err != nil {
					t.Fatalf("failed to format template: %v", err)
				}

				goldenPath := filepath.Join(goldenDirOut, baseName)

				if *update {
					if err := os.WriteFile(goldenPath, []byte(output), 0o644); err != nil {
						t.Fatalf("failed to write golden file: %v", err)
					}
				} else {
					expected, err := os.ReadFile(goldenPath)
					if err != nil {
						t.Fatalf("failed to read golden file: %v", err)
					}
					if output != toUnixLineEndings(string(expected)) {
						t.Errorf("output does not match golden file.\nGot:\n%s\nExpected:\n%s", output, expected)
					}

					// Format output again to check for idempotency.
					output2, err := Format(output)
					if err != nil {
						t.Fatalf("failed to format output again: %v", err)
					}
					if output != output2 {
						t.Errorf("output is not idempotent.\nFirst format:\n%s\nSecond format:\n%s", output, output2)
					}

					// Try parsing the output with Go's text/template to ensure it's valid.
					tryParseGoTextTemplate(t, output)
				}
			}

			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read input file: %v", err)
			}
			s := toUnixLineEndings(string(b))

			testFormat(s)
			if !*update {
				testFormat(toWindowsLineEndings(s))
			}
		})
		return nil
	}); err != nil {
		t.Fatalf("failed to walk golden/in: %v", err)
	}
}

func tryParseGoTextTemplate(t *testing.T, text string) {
	// Needed for validation.
	fn := func() string {
		return "test"
	}
	funcMap := template.FuncMap{
		"cond":        fn,
		"css":         fn,
		"dict":        fn,
		"hugo":        fn,
		"js":          fn,
		"resources":   fn,
		"site":        fn,
		"fingerprint": fn,
		"safeCSS":     fn,
		"append":      fn,
		"errorf":      fn,
		"diagrams":    fn,
		"default":     fn,
	}

	_, err := template.New("").Funcs(funcMap).Parse(text)
	if err != nil {
		t.Fatal("Error parsing template:", err)
	}
}

func toUnixLineEndings(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func toWindowsLineEndings(s string) string {
	return strings.ReplaceAll(s, "\n", "\r\n")
}

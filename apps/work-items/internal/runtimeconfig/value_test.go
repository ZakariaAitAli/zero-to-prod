package runtimeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secretValue = "postgres://user:canary-secret@example/db"

func lookupFrom(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func writeFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	return path
}

func TestValueReadsEnvironment(t *testing.T) {
	value, err := Value(lookupFrom(map[string]string{"SETTING": secretValue}), "SETTING")
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}

	if value != secretValue {
		t.Fatalf("unexpected value %q", value)
	}
}

func TestValueReadsFileAndTrimsTrailingNewline(t *testing.T) {
	path := writeFile(t, secretValue+"\n")

	value, err := Value(lookupFrom(map[string]string{"SETTING_FILE": path}), "SETTING")
	if err != nil {
		t.Fatalf("read setting file: %v", err)
	}

	if value != secretValue {
		t.Fatalf("unexpected value %q", value)
	}
}

func TestValueRejectsInvalidSources(t *testing.T) {
	emptyPath := writeFile(t, "\n")
	secretPath := writeFile(t, secretValue)
	missingPath := filepath.Join(t.TempDir(), "missing")

	tests := []struct {
		name    string
		values  map[string]string
		message string
	}{
		{
			name:    "neither source",
			values:  map[string]string{},
			message: "SETTING is required (or set SETTING_FILE)",
		},
		{
			name:    "both sources",
			values:  map[string]string{"SETTING": secretValue, "SETTING_FILE": secretPath},
			message: "SETTING and SETTING_FILE are both set",
		},
		{
			name:    "missing file",
			values:  map[string]string{"SETTING_FILE": missingPath},
			message: "read SETTING_FILE:",
		},
		{
			name:    "empty file",
			values:  map[string]string{"SETTING_FILE": emptyPath},
			message: "is empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Value(lookupFrom(test.values), "SETTING")
			if err == nil {
				t.Fatal("invalid source unexpectedly accepted")
			}

			if !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q in error, got %v", test.message, err)
			}

			if strings.Contains(err.Error(), "canary-secret") {
				t.Fatalf("error exposes the setting value: %v", err)
			}
		})
	}
}

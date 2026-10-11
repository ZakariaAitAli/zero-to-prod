// Package runtimeconfig reads runtime settings that may carry credentials.
package runtimeconfig

import (
	"fmt"
	"os"
	"strings"
)

// Value returns the setting called name, taken either directly from the
// environment variable name or from the file named by name_FILE.
//
// Exactly one source must be set. The file form lets a runtime deliver a
// credential-bearing setting without placing it in the process environment.
// Errors name the setting and file path but never include the value.
func Value(lookup func(string) string, name string) (string, error) {
	fileName := name + "_FILE"

	direct := lookup(name)
	path := lookup(fileName)

	switch {
	case direct != "" && path != "":
		return "", fmt.Errorf("%s and %s are both set; set only one", name, fileName)

	case direct != "":
		return direct, nil

	case path == "":
		return "", fmt.Errorf("%s is required (or set %s)", name, fileName)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileName, err)
	}

	value := strings.TrimRight(string(contents), "\r\n")
	if value == "" {
		return "", fmt.Errorf("%s file %s is empty", fileName, path)
	}

	return value, nil
}

//go:build windows

package checks

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

//go:embed tools/osqueryi.exe
var osqueryiBinary []byte

// runOsquerySQL extracts the embedded osqueryi binary, executes the given SQL,
// and returns the parsed JSON rows. The caller does not need to manage the
// temporary executable — it is cleaned up before this function returns.
func runOsquerySQL(sql string) ([]map[string]string, error) {
	tmpExe, err := extractEmbeddedBinary(osqueryiBinary, "osqueryi-*.exe")
	if err != nil {
		return nil, fmt.Errorf("extract osqueryi: %w", err)
	}
	defer os.Remove(tmpExe)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(tmpExe, "--json", sql)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("osqueryi: %v — %s", err, stderr.String())
	}

	var rows []map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rows); err != nil {
		return nil, fmt.Errorf("parse osqueryi output: %w", err)
	}
	return rows, nil
}

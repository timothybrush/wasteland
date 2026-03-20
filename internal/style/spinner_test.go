package style

import (
	"bytes"
	"os"
	"testing"
)

func TestStartSpinner_NonTTYBufferWritesMessage(t *testing.T) {
	var buf bytes.Buffer

	spinner := StartSpinner(&buf, "Loading wanted items")
	spinner.Stop()
	spinner.Stop()

	if got := buf.String(); got != "Loading wanted items\n" {
		t.Fatalf("buffer = %q, want one-line message", got)
	}
}

func TestStartSpinner_NonTTYFileWritesMessage(t *testing.T) {
	file, err := os.CreateTemp("", "spinner-test-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()
	defer file.Close() //nolint:errcheck

	spinner := StartSpinner(file, "Syncing remotes")
	spinner.Stop()

	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "Syncing remotes\n" {
		t.Fatalf("file = %q, want one-line message", got)
	}
}

package store

import (
	"sync"
	"testing"
)

// testDBMu serializes integration tests that TRUNCATE the shared
// MEMORY_TEST_DATABASE_URL. go test ./... runs packages in parallel;
// without this lock, api/mcp/lint/importclaude/store tests race on one DB.
var testDBMu sync.Mutex

// LockTestDB holds the shared integration-test database for the lifetime of t.
func LockTestDB(t *testing.T) {
	t.Helper()
	testDBMu.Lock()
	t.Cleanup(testDBMu.Unlock)
}

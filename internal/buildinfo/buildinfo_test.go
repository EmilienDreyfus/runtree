package buildinfo

import "testing"

func TestSummaryAndDetails(t *testing.T) {
	previousVersion := Version
	previousCommit := Commit
	previousDate := Date
	t.Cleanup(func() {
		Version = previousVersion
		Commit = previousCommit
		Date = previousDate
	})

	Version = "v0.1.0"
	Commit = "abc123"
	Date = "2026-05-07T12:00:00Z"

	if got := Summary(); got != "runtree v0.1.0" {
		t.Fatalf("Summary() = %q", got)
	}

	want := "runtree v0.1.0\ncommit: abc123\nbuilt: 2026-05-07T12:00:00Z"
	if got := Details(); got != want {
		t.Fatalf("Details() = %q, want %q", got, want)
	}
}

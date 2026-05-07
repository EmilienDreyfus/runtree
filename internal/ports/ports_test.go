package ports

import "testing"

func TestAllocateSkipsReservedAndBusyPorts(t *testing.T) {
	t.Parallel()

	busyPort := 49100
	start := busyPort
	end := busyPort + 2
	reserved := map[int]bool{busyPort + 1: true}

	port, err := AllocateWithChecker(start, end, reserved, func(port int) bool {
		return port != busyPort
	})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if port != busyPort+2 {
		t.Fatalf("Allocate() = %d, want %d (busy=%d reserved=%d)", port, busyPort+2, busyPort, busyPort+1)
	}
}

func TestAllocateFailsWhenRangeExhausted(t *testing.T) {
	t.Parallel()

	start := 49000
	end := 49000

	if _, err := AllocateWithChecker(start, end, map[int]bool{}, func(int) bool { return false }); err == nil {
		t.Fatal("Allocate() error = nil, want exhausted range error")
	}
}

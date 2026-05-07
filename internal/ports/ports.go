package ports

import (
	"fmt"
	"net"
)

func Allocate(start, end int, reserved map[int]bool) (int, error) {
	return AllocateWithChecker(start, end, reserved, IsAvailable)
}

func AllocateWithChecker(start, end int, reserved map[int]bool, checker func(int) bool) (int, error) {
	if checker == nil {
		checker = IsAvailable
	}
	for port := start; port <= end; port++ {
		if reserved[port] {
			continue
		}
		if !checker(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no available port in range %d-%d", start, end)
}

func IsAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

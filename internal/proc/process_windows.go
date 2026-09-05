package proc

import "os"

// Task 6 supplies creation-time Job membership before Windows probes are enabled.
type process struct{}

func startProcess(Request, *os.File, *os.File) (*process, error) { return nil, ErrUnsupported }
func (*process) wait() error                                     { return ErrUnsupported }
func (*process) kill() error                                     { return ErrUnsupported }
func (*process) exitCode() int                                   { return -1 }

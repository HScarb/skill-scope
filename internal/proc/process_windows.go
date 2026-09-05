package proc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinBase.h defines ProcThreadAttributeJobList as 13, with the input flag
// 0x00020000: https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/WinBase.h
const procThreadAttributeJobList = 0x0002000d

// ContainmentError means Windows could not create the probe inside its Job.
// No uncontained command is started as a fallback.
type ContainmentError struct {
	Operation string
	Cause     error
}

func (e *ContainmentError) Error() string {
	return fmt.Sprintf("auxiliary process Job %s: %v", e.Operation, e.Cause)
}
func (e *ContainmentError) Unwrap() error { return e.Cause }

type process struct {
	handle windows.Handle
	job    windows.Handle
	code   uint32
}

func startProcess(req Request, stdout, stderr *os.File) (*process, error) {
	if ext := filepath.Ext(req.Executable); strings.EqualFold(ext, ".cmd") || strings.EqualFold(ext, ".bat") {
		return nil, errors.New("auxiliary commands on Windows require a real .exe; command scripts are not supported")
	}
	app, err := windows.UTF16PtrFromString(req.Executable)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{req.Executable}, req.Args...)))
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if req.Dir != "" {
		dir, err = windows.UTF16PtrFromString(req.Dir)
		if err != nil {
			return nil, err
		}
	}
	for _, entry := range req.Env {
		if strings.ContainsRune(entry, 0) {
			return nil, errors.New("auxiliary command environment contains a NUL")
		}
	}
	// CreateProcess requires a sorted, double-NUL terminated Unicode block.
	// Even an empty environment has a non-nil block, preventing inheritance.
	sort.SliceStable(req.Env, func(i, j int) bool { return strings.ToUpper(req.Env[i]) < strings.ToUpper(req.Env[j]) })
	env := utf16.Encode([]rune(strings.Join(req.Env, "\x00") + "\x00\x00"))
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, &ContainmentError{Operation: "create", Cause: err}
	}
	started := false
	defer func() {
		if !started {
			_ = windows.CloseHandle(job)
		}
	}()
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, &ContainmentError{Operation: "configure", Cause: err}
	}
	attrs, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, &ContainmentError{Operation: "attributes", Cause: err}
	}
	defer attrs.Delete()
	if err := attrs.Update(procThreadAttributeJobList, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return nil, &ContainmentError{Operation: "bind", Cause: err}
	}
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	defer stdin.Close()
	// Duplicate only the three standard handles; never change inheritance on
	// the caller's handles or leak unrelated inheritable handles into the probe.
	var handles [3]windows.Handle
	defer func() {
		for _, handle := range handles {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
		}
	}()
	for i, file := range []*os.File{stdin, stdout, stderr} {
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &handles[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return nil, err
		}
	}
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), unsafe.Sizeof(handles)); err != nil {
		return nil, err
	}
	si := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES,
			StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2],
		},
		ProcThreadAttributeList: attrs.List(),
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_NO_WINDOW)
	err = windows.CreateProcess(app, commandLine, nil, nil, true, flags, &env[0], dir, &si.StartupInfo, &pi)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(job)
	if err != nil {
		return nil, &ContainmentError{Operation: "create process", Cause: err}
	}
	_ = windows.CloseHandle(pi.Thread)
	started = true
	return &process{handle: pi.Process, job: job}, nil
}

func (p *process) wait() (err error) {
	defer func() { err = errors.Join(err, windows.CloseHandle(p.handle)) }()
	if _, err := windows.WaitForSingleObject(p.handle, windows.INFINITE); err != nil {
		return err
	}
	if err := windows.GetExitCodeProcess(p.handle, &p.code); err != nil {
		return err
	}
	if p.code != 0 {
		return errors.New("auxiliary process returned a nonzero exit code")
	}
	return nil
}

func (p *process) exitCode() int { return int(p.code) }

func (p *process) kill() error {
	// The runner calls this exactly once, including after the direct child
	// exits, to terminate remaining descendants before joining pipe readers.
	err := windows.TerminateJobObject(p.job, 1)
	closeErr := windows.CloseHandle(p.job)
	return errors.Join(err, closeErr)
}

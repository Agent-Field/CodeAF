//go:build windows

package processgroup

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A handle, rather than a parent PID chain, owns descendants even after their
// parents exit. Captured Groups keep this record after its PID is recycled.
type jobObject struct {
	mu      sync.Mutex
	handle  windows.Handle
	process windows.Handle
	started time.Time
}

var launches = struct {
	sync.Mutex
	jobs map[int]*jobObject
}{jobs: make(map[int]*jobObject)}

var (
	ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
	openJobObject   = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")
)

func jobName(pid int, started time.Time) *uint16 {
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`Local\codeaf-process-%d-%d`, pid, started.UnixNano()))
	return name
}

func launchJob(pid int) *jobObject {
	launches.Lock()
	defer launches.Unlock()
	if job := launches.jobs[pid]; job != nil {
		return job
	}
	// A deliberately kept service can be stopped by a later engine. Its native
	// creation time gates opening the named job, so a recycled PID is refused.
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil
	}
	started, err := processStartedAt(process)
	if err != nil {
		windows.CloseHandle(process)
		return nil
	}
	name := jobName(pid, started)
	handle, _, _ := openJobObject.Call(0x0004|0x0008, 0, uintptr(unsafe.Pointer(name))) // JOB_OBJECT_QUERY | JOB_OBJECT_TERMINATE
	if handle == 0 {
		windows.CloseHandle(process)
		return nil
	}
	job := &jobObject{handle: windows.Handle(handle), process: process, started: started}
	launches.jobs[pid] = job
	go watchJob(pid, job)
	return job
}

// beforeJobAssignment lets the native regression cancel at the assignment
// boundary. Production leaves it nil.
var beforeJobAssignment func()

// Start assigns the suspended child before any shim or descendant can run.
// A failed assignment or resume fails the launch and reaps the suspended child.
func Start(cmd *exec.Cmd) error {
	// Cmd.Start starts its cancellation watcher before returning. Keep that
	// watcher behind setup so it cannot see an empty job and lose the stop.
	launches.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			launches.Unlock()
		}
	}
	defer unlock()
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	flags := cmd.SysProcAttr.CreationFlags
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	err := cmd.Start()
	cmd.SysProcAttr.CreationFlags = flags
	if err != nil {
		return err
	}
	pid := cmd.Process.Pid
	job := &jobObject{}
	fail := func(err error) error {
		unlock()
		if job.handle != 0 {
			_ = windows.TerminateJobObject(job.handle, 1)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if job.process != 0 {
			windows.CloseHandle(job.process)
		}
		if job.handle != 0 {
			windows.CloseHandle(job.handle)
		}
		return fmt.Errorf("assign and resume process job: %w", err)
	}
	job.process, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_DUP_HANDLE, false, uint32(pid))
	if err != nil {
		return fail(err)
	}
	job.started, err = processStartedAt(job.process)
	if err != nil {
		return fail(err)
	}
	job.handle, err = windows.CreateJobObject(nil, jobName(pid, job.started))
	if err != nil {
		return fail(err)
	}
	detached := flags&windows.DETACHED_PROCESS != 0
	// Setpgid and Setsid do not kill on parent exit. Keep that lifetime here:
	// managed quit paths explicitly stop their jobs; ordinary background
	// children and deliberate detaches may outlive the launching engine.
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	if _, err := windows.SetInformationJobObject(job.handle, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fail(err)
	}
	if beforeJobAssignment != nil {
		beforeJobAssignment()
	}
	if err := windows.AssignProcessToJobObject(job.handle, job.process); err != nil {
		return fail(err)
	}
	if detached {
		// This unused handle lives exactly as long as the detached leader. It keeps
		// the named job open for re-adoption after the launching engine quits.
		var remote windows.Handle
		if err := windows.DuplicateHandle(windows.CurrentProcess(), job.handle, job.process, &remote, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return fail(err)
		}
	}
	launches.jobs[pid] = job
	status, _, _ := ntResumeProcess.Call(uintptr(job.process))
	if status != 0 {
		delete(launches.jobs, pid)
		return fail(fmt.Errorf("NtResumeProcess status %#x", status))
	}
	unlock()
	go watchJob(pid, job)
	return nil
}

// Wait belongs to the caller. The job and process handles stay open until the
// last descendant exits. Keeping the leader handle also prevents PID reuse.
func watchJob(pid int, job *jobObject) {
	for job.alive() {
		time.Sleep(200 * time.Millisecond)
	}
	launches.Lock()
	if launches.jobs[pid] == job {
		delete(launches.jobs, pid)
	}
	launches.Unlock()
}

// The SDK exposes this accounting class but not its fixed-width structure.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime                                                 int64
	ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime                             int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
}

func (j *jobObject) alive() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return false
	}
	var accounting jobAccounting
	err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	if err != nil || accounting.ActiveProcesses != 0 {
		return true
	}
	windows.CloseHandle(j.handle)
	windows.CloseHandle(j.process)
	j.handle = 0
	return false
}

// hardKillWait bounds confirmation for callers that only use Kill, including
// command cancellation; leader reaping alone cannot prove tree completion.
const hardKillWait = 2 * time.Second

func (j *jobObject) kill() error {
	j.mu.Lock()
	if j.handle == 0 {
		j.mu.Unlock()
		return nil
	}
	err := windows.TerminateJobObject(j.handle, 1)
	j.mu.Unlock()
	if err != nil {
		return err
	}
	return j.waitEmpty(hardKillWait)
}

func (j *jobObject) waitEmpty(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for j.alive() {
		if timeout > 0 && !time.Now().Before(deadline) {
			return fmt.Errorf("process tree is still running")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func (j *jobObject) terminate(pid int) error {
	// Keep the existing single taskkill /T soft round. The job owns the hard
	// sweep after the grace, including descendants whose parent has exited.
	// Holding this handle also pins the leader PID while taskkill uses it.
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return nil
	}
	return taskkill(pid, false)
}

// StartedAt also answers after a tracked leader exits, while its tree lives.
func StartedAt(pid int) (time.Time, error) {
	if job := launchJob(pid); job != nil {
		return job.started, nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, err
	}
	defer windows.CloseHandle(handle)
	return processStartedAt(handle)
}

func processStartedAt(handle windows.Handle) (time.Time, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, created.Nanoseconds()).UTC(), nil
}

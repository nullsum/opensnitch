package procmon

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/gustavo-iniguez-goya/opensnitch/daemon/core"
	"github.com/gustavo-iniguez-goya/opensnitch/daemon/log"
	"github.com/gustavo-iniguez-goya/opensnitch/daemon/procmon/audit"
)

func getPIDFromAuditEvents(inode int, inodeKey string, expect string) (int, int) {
	audit.Lock.RLock()
	defer audit.Lock.RUnlock()

	auditEvents := audit.GetEvents()
	for n := 0; n < len(auditEvents); n++ {
		pid := auditEvents[n].Pid
		if inodeFound("/proc/", expect, inodeKey, inode, pid) {
			return pid, n
		}
	}
	for n := 0; n < len(auditEvents); n++ {
		ppid := auditEvents[n].PPid
		if inodeFound("/proc/", expect, inodeKey, inode, ppid) {
			return ppid, n
		}
	}
	return -1, -1
}

// GetPIDFromINode tries to get the PID from a socket inode follwing these steps:
// 1. Get the PID from the cache of Inodes.
// 2. Get the PID from the cache of PIDs.
// 3. Look for the PID using one of these methods:
//    - ftrace: listening processes execs/exits from /sys/kernel/debug/tracing/
//    - audit:  listening for socket creation from auditd.
//    - proc:   search /proc
//
// If the PID is not found by one of the 2 first methods, it'll try it using /proc.
func GetPIDFromINode(inode int, inodeKey string) int {
	found := -1
	if inode <= 0 {
		return found
	}
	start := time.Now()
	cleanUpCaches()

	expect := fmt.Sprintf("socket:[%d]", inode)
	if cachedPidInode := getPidByInodeFromCache(inodeKey); cachedPidInode != -1 {
		log.Debug("Inode found in cache", time.Since(start), inodesCache[inodeKey], inode, inodeKey)
		return cachedPidInode
	}

	cachedPid, pos := getPidFromCache(inode, inodeKey, expect)
	if cachedPid != -1 {
		log.Debug("Socket found in known pids %v, pid: %d, inode: %d, pids in cache: %d", time.Since(start), cachedPid, inode, "pos", pos, len(pidsCache))
		sortProcEntries()
		return cachedPid
	}

	if MonitorMethod == MethodAudit {
		if aPid, pos := getPIDFromAuditEvents(inode, inodeKey, expect); aPid != -1 {
			log.Debug("PID found via audit events", time.Since(start), "position", pos)
			return aPid
		}
	} else if MonitorMethod == MethodFtrace && IsWatcherAvailable() {
		forEachProcess(func(pid int, path string, args []string) bool {
			if inodeFound("/proc/", expect, inodeKey, inode, pid) {
				found = pid
				return true
			}
			// keep looping
			return false
		})
	}
	if found == -1 || MonitorMethod == MethodProc {
		found = lookupPidInProc("/proc/", expect, inodeKey, inode)
	}
	log.Debug("new pid lookup took", found, time.Since(start))

	return found
}

func cleanPath(proc *Process) {
	pathLen := len(proc.Path)
	if pathLen >= 10 && proc.Path[pathLen-10:] == " (deleted)" {
		proc.Path = proc.Path[:len(proc.Path)-10]
	}
}

func parseCmdLine(proc *Process) {
	if data, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/cmdline", proc.ID)); err == nil {
		for i, b := range data {
			if b == 0x00 {
				data[i] = byte(' ')
			}
		}

		args := strings.Split(string(data), " ")
		for _, arg := range args {
			arg = core.Trim(arg)
			if arg != "" {
				proc.Args = append(proc.Args, arg)
			}
		}
	}
}

func parseCWD(proc *Process) {
	if link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", proc.ID)); err == nil {
		proc.CWD = link
	}
}

func parseEnv(proc *Process) {
	if data, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/environ", proc.ID)); err == nil {
		for _, s := range strings.Split(string(data), "\x00") {
			parts := strings.SplitN(core.Trim(s), "=", 2)
			if parts != nil && len(parts) == 2 {
				key := core.Trim(parts[0])
				val := core.Trim(parts[1])
				proc.Env[key] = val
			}
		}
	}
}

// FindProcess checks if a process exists given a PID.
// If it exists in /proc, a new Process{} object is returned with  the details
// to identify a process (cmdline, name, environment variables, etc).
func FindProcess(pid int, interceptUnknown bool) *Process {
	if interceptUnknown && pid < 0 {
		return NewProcess(0, "")
	}
	if MonitorMethod == MethodAudit {
		if aevent := audit.GetEventByPid(pid); aevent != nil {
			audit.Lock.RLock()
			proc := NewProcess(pid, aevent.ProcPath)
			proc.Args = strings.Split(strings.Replace(aevent.ProcCmdLine, "\x00", " ", -1), " ")
			proc.CWD = aevent.ProcDir
			audit.Lock.RUnlock()
			parseEnv(proc)
			cleanPath(proc)

			return proc
		}
	}

	linkName := fmt.Sprint("/proc/", pid, "/exe")
	if _, err := os.Lstat(linkName); err != nil {
		return nil
	}

	if link, err := os.Readlink(linkName); err == nil {
		proc := NewProcess(pid, link)

		parseCmdLine(proc)
		parseCWD(proc)
		parseEnv(proc)
		cleanPath(proc)

		return proc
	}
	return nil
}

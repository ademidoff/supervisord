package process

import (
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/ochinchina/supervisord/config"
	log "github.com/sirupsen/logrus"
)

// Manager manage all the process in the supervisor
type Manager struct {
	procs          map[string]*Process
	eventListeners map[string]*Process
	lock           sync.Mutex
	stopMonitor    chan bool
}

// NewManager creates new Manager object
func NewManager() *Manager {
	return &Manager{procs: make(map[string]*Process),
		eventListeners: make(map[string]*Process),
		stopMonitor:    make(chan bool),
	}
}

// CreateProcess creates process (program or event listener) and adds to Manager object
func (pm *Manager) CreateProcess(supervisorID string, config *config.Entry) *Process {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	if config.IsProgram() {
		return pm.createProgram(supervisorID, config)
	} else if config.IsEventListener() {
		return pm.createEventListener(supervisorID, config)
	} else {
		return nil
	}
}

// StartAutoStartPrograms starts all programs that set as should be autostarted
// This method respects dependencies and starts processes in proper order
func (pm *Manager) StartAutoStartPrograms() {
	// First, start all processes without dependencies
	pm.ForEachProcess(func(proc *Process) {
		if proc.isAutoStart() && !proc.HasDependencies() {
			log.WithFields(log.Fields{"program": proc.GetName()}).Info("Starting process without dependencies")
			proc.Start(false)
		}
	})

	// Then monitor and start dependent processes when their dependencies are stable
	log.Info("Starting dependency monitor goroutine")
	go pm.monitorDependentProcesses()
}

// monitorDependentProcesses continuously monitors and starts processes when their dependencies are stable
func (pm *Manager) monitorDependentProcesses() {
	log.Info("Dependency monitor started")
	ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Debug("Checking dependent processes - monitoring cycle")
			foundDependentProcs := 0

			// First, collect all processes in a map to avoid locking issues
			pm.lock.Lock()
			processMap := make(map[string]*Process)
			maps.Copy(processMap, pm.procs)
			pm.lock.Unlock()

			// Now check dependencies without holding the lock
			for _, proc := range processMap {
				// Log all processes for debugging
				log.WithFields(log.Fields{"program": proc.GetName(), "autoStart": proc.isAutoStart(), "hasDeps": proc.HasDependencies(), "state": proc.GetState()}).Debug("Process status")

				// Only consider processes that:
				// 1. Are set to autostart
				// 2. Have dependencies
				// 3. Are not currently running
				// 4. Are not currently starting
				if proc.isAutoStart() && proc.HasDependencies() &&
					proc.GetState() != Running && proc.GetState() != Starting {

					foundDependentProcs++
					log.WithFields(log.Fields{"program": proc.GetName(), "state": proc.GetState(), "dependencies": proc.GetDependencies()}).Debug("Checking dependent process")

					if pm.areDependenciesStableWithMap(proc, processMap) {
						log.WithFields(log.Fields{"program": proc.GetName()}).Info("Dependencies are stable, starting dependent process")
						proc.Start(false)
					}
				}
			}
			log.WithFields(log.Fields{"count": foundDependentProcs}).Debug("Found dependent processes to check")
		case <-pm.stopMonitor:
			log.Info("Stopping monitor for dependent processes")
			return
		}
	}
}

func (pm *Manager) createProgram(supervisorID string, config *config.Entry) *Process {
	procName := config.GetProgramName()

	proc, ok := pm.procs[procName]

	if !ok {
		proc = NewProcess(supervisorID, config)
		// Set up callback to handle dependency state changes
		proc.SetStateChangeCallback(pm.onProcessStateChange)
		pm.procs[procName] = proc
	}
	log.WithField("program", procName).Info("Create process")
	return proc
}

func (pm *Manager) createEventListener(supervisorID string, config *config.Entry) *Process {
	eventListenerName := config.GetEventListenerName()

	evtListener, ok := pm.eventListeners[eventListenerName]

	if !ok {
		evtListener = NewProcess(supervisorID, config)
		// Set up callback to handle dependency state changes
		evtListener.SetStateChangeCallback(pm.onProcessStateChange)
		pm.eventListeners[eventListenerName] = evtListener
	}
	log.Info("create event listener:", eventListenerName)
	return evtListener
}

// Add process to Manager object
func (pm *Manager) Add(name string, proc *Process) {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	pm.procs[name] = proc
	log.Info("add process:", name)
}

// Remove process from Manager object
//
// Arguments:
// name - the name of program
//
// Return the process or nil
func (pm *Manager) Remove(name string) *Process {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	proc, _ := pm.procs[name]
	delete(pm.procs, name)
	log.Info("remove process:", name)
	return proc
}

// Find process by program name. Returns process or nil if process is not listed in Manager object
func (pm *Manager) Find(name string) *Process {
	procs := pm.FindMatch(name)
	if len(procs) == 1 {
		if procs[0].GetName() == name || name == fmt.Sprintf("%s:%s", procs[0].GetGroup(), procs[0].GetName()) {
			return procs[0]
		}
	}
	return nil
}

// FindMatch lookup program with one of following format:
// - group:program
// - group:*
// - program
func (pm *Manager) FindMatch(name string) []*Process {
	result := make([]*Process, 0)
	if pos := strings.Index(name, ":"); pos != -1 {
		groupName := name[0:pos]
		programName := name[pos+1:]
		pm.ForEachProcess(func(p *Process) {
			if p.GetGroup() == groupName {
				if programName == "*" || programName == p.GetName() {
					result = append(result, p)
				}
			}
		})
	} else {
		pm.lock.Lock()
		defer pm.lock.Unlock()
		proc, ok := pm.procs[name]
		if ok {
			result = append(result, proc)
		}
	}
	if len(result) <= 0 {
		log.Info("fail to find process:", name)
	}
	return result
}

// Clear all the processes from Manager object
func (pm *Manager) Clear() {
	pm.lock.Lock()
	defer pm.lock.Unlock()
	pm.procs = make(map[string]*Process)
}

// ForEachProcess process each process in sync mode
func (pm *Manager) ForEachProcess(procFunc func(p *Process)) {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	procs := pm.getAllProcess()
	for _, proc := range procs {
		procFunc(proc)
	}
}

// AsyncForEachProcess handle each process in async mode
// Args:
// - procFunc, the function to handle the process
// - done, signal the process is completed
// Returns: number of total processes
func (pm *Manager) AsyncForEachProcess(procFunc func(p *Process), done chan *Process) int {
	pm.lock.Lock()
	defer pm.lock.Unlock()

	procs := pm.getAllProcess()

	for _, proc := range procs {
		go forOneProcess(proc, procFunc, done)
	}
	return len(procs)
}

func forOneProcess(proc *Process, action func(p *Process), done chan *Process) {
	action(proc)
	done <- proc
}

func (pm *Manager) getAllProcess() []*Process {
	tmpProcs := make([]*Process, 0)
	for _, proc := range pm.procs {
		tmpProcs = append(tmpProcs, proc)
	}
	return sortProcess(tmpProcs)
}

// StopAllProcesses stop all the processes listed in Manager object
func (pm *Manager) StopAllProcesses() {
	var wg sync.WaitGroup

	pm.ForEachProcess(func(proc *Process) {
		wg.Add(1)

		go func(wg *sync.WaitGroup) {
			defer wg.Done()

			proc.Stop(true)
		}(&wg)
	})

	wg.Wait()
}

func sortProcess(procs []*Process) []*Process {
	progConfigs := make([]*config.Entry, 0)
	for _, proc := range procs {
		if proc.config.IsProgram() {
			progConfigs = append(progConfigs, proc.config)
		}
	}

	result := make([]*Process, 0)
	p := config.NewProcessSorter()
	for _, config := range p.SortProgram(progConfigs) {
		for _, proc := range procs {
			if proc.config == config {
				result = append(result, proc)
			}
		}
	}

	return result
}

// areDependenciesStableWithMap checks if all dependencies are in stable running state
// using a provided process map to avoid locking issues (using each dependency's own startsecs)
func (pm *Manager) areDependenciesStableWithMap(proc *Process, processMap map[string]*Process) bool {
	dependencies := proc.GetDependencies()
	if len(dependencies) == 0 {
		return true // No dependencies means all dependencies are satisfied
	}

	now := time.Now()

	log.WithFields(log.Fields{"program": proc.GetName(), "dependencies": dependencies}).Debug("Checking dependency stability")

	for _, depName := range dependencies {
		log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName}).Debug("Checking individual dependency")

		depProc, ok := processMap[depName]
		if !ok {
			log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName}).Debug("Dependency not found")
			return false
		}

		// Use the dependency's own startsecs for stability checking
		depStabilityTime := depProc.GetDependencyStabilityTime()

		log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName, "depState": depProc.GetState(), "depStabilityTime": depStabilityTime}).Debug("Found dependency")

		// Check if dependency is running
		if depProc.GetState() != Running {
			log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName, "depState": depProc.GetState()}).Debug("Dependency not in running state")
			return false
		}

		// Check if dependency has been stable for its own required time
		runTime := now.Sub(depProc.GetStartTime())
		if runTime.Seconds() < float64(depStabilityTime) {
			log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName, "runTime": runTime.Seconds(), "requiredTime": depStabilityTime}).Debug("Dependency not stable for its required time")
			return false
		}

		log.WithFields(log.Fields{"program": proc.GetName(), "dependency": depName, "runTime": runTime.Seconds(), "requiredTime": depStabilityTime}).Debug("Dependency is stable")
	}

	log.WithFields(log.Fields{"program": proc.GetName()}).Debug("All dependencies are stable")
	return true
}

// StopDependentProcesses stops all processes that depend on the given process
func (pm *Manager) StopDependentProcesses(failedProcName string) {
	pm.ForEachProcess(func(proc *Process) {
		dependencies := proc.GetDependencies()
		for _, dep := range dependencies {
			if dep == failedProcName && (proc.GetState() == Running || proc.GetState() == Starting) {
				log.WithFields(log.Fields{"program": proc.GetName(), "dependency": failedProcName}).Info("Stopping dependent process because dependency failed")
				proc.Stop(false)
			}
		}
	})
}

// onProcessStateChange handles process state changes and manages dependencies
func (pm *Manager) onProcessStateChange(proc *Process, oldState State, newState State) {
	procName := proc.GetName()

	// If a process goes into Fatal state, stop dependent processes
	if newState == Fatal {
		// Only stop dependents if this was an unexpected failure (not a user-initiated stop)
		if !proc.IsStoppedByUser() {
			log.WithFields(log.Fields{"program": procName, "state": newState}).Info("Process failed, stopping dependent processes")
			pm.StopDependentProcesses(procName)
		}
	}

	// If a process enters Running state, check if any dependent processes can now start
	if newState == Running {
		log.WithFields(log.Fields{"program": procName}).Debug("Process is now running, checking dependent processes")
		// The monitorDependentProcesses goroutine will handle starting dependent processes
	}
}

// StopDependencyMonitor stops the dependency monitoring goroutine
func (pm *Manager) StopDependencyMonitor() {
	select {
	case pm.stopMonitor <- true:
	default:
		// Channel might be closed or full, ignore
	}
}

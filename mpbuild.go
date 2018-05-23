package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jessevdk/go-flags"
)

var gOpts struct {
	// Slice of bool will append 'true' each time the option
	// is encountered (can be set multiple times, like -vvv)
	Verbose     []bool   `short:"v" long:"verbose" description:"Show verbose debug information"`
	Config      []string `short:"c" long:"config" description:"Specify multiple Debug or Release (default both)"`
	Log         string   `short:"l" long:"log" description:"Log file"`
	Ios         bool     `short:"i" long:"ios" description:"ios build"`
	Quiet       bool     `short:"q" long:"quiet" description:"Suppress most xcodebuild output"`
	Start       string   `short:"s" long:"start" description:"Start at project <search>"`
	Only        string   `short:"o" long:"only" description:"Optional comma separated list of projects"`
	Deps        string   `short:"d" long:"deps" description:"Optional comma separated list of projects"`
	Not         string   `short:"n" long:"not" description:"Optional comma separated list of projects"`
	UI          bool     `short:"u" long:"ui" description:"Show a UI for tracking distcc/xcode activity"`
	ContinueErr bool     `short:"C" long:"continue" description:"Continue on error"`
	ListPlugins bool     `long:"listplugins" description:"List all plugins"`
}

// Job ...
type Job struct {
	Tasks    []*Task `json:"tasks"`
	Platform string  `json:"platform_type,omitempty"`
}

var gVS2017 = `C:\Program Files (x86)\Microsoft Visual Studio\2017\Common7\IDE`
var gVS2017Ent = `C:\Program Files (x86)\Microsoft Visual Studio\2017\Enterprise\Common7\IDE`
var gVS2017Ult = `C:\Program Files (x86)\Microsoft Visual Studio\2017\Ultimate\Common7\IDE`
var gVS2015 = `C:\Program Files (x86)\Microsoft Visual Studio 14.0\Common7\IDE`

func checkWinCompiler() (path string) {
	if _, err := os.Stat(gVS2017); os.IsNotExist(err) {
		if _, err := os.Stat(gVS2017Ent); os.IsNotExist(err) {
			if _, err := os.Stat(gVS2017Ult); os.IsNotExist(err) {
				if _, err := os.Stat(gVS2015); os.IsNotExist(err) {
					log.Panic("Could not find VisualStudio!")
				} else {
					return gVS2015
				}
			} else {
				return gVS2017Ult
			}
		} else {
			return gVS2017Ent
		}
	}
	return gVS2017

}

// Search ...
func (j *Job) Search(project string) int {
	for cnt, task := range j.Tasks {
		if strings.Contains(task.Messages, project) {
			return cnt
		}
	}
	return -1
}

// Task ...
type Task struct {
	Inputs   []int  `json:"inputs"`
	Cost     int    `json:"cost"`
	Messages string `json:"messages"`
	MadeProj string `json:"made_proj"`
	ID       int    `json:"id"`
	Complete int32
	Running  bool
	Err      error
	Output   string
	Start    time.Time
}

// IsCompleted ...
func (t *Task) IsCompleted() bool {
	return atomic.LoadInt32(&t.Complete) != 0
}

// SetCompleted ...
func (t *Task) SetCompleted() {
	t.Running = true
	atomic.StoreInt32(&t.Complete, 1)
}

// HasPendingDeps ...
func (t *Task) HasPendingDeps(job *Job) bool {
	for _, input := range t.Inputs {
		task := job.Tasks[input]
		if !task.IsCompleted() {
			//fmt.Printf("  %s depends on %s\n", t.Messages, task.Messages)
			return true
		}
	}
	return false
}

// DependsOn ...
func (t *Task) DependsOn(ID int) bool {
	for _, input := range t.Inputs {
		if ID == input {
			return true
		}
	}
	return false
}

func logError(task *Task, msg string, err error) {
	var s = "mpbuild: " + msg + " (%s) error: %v\n"
	log.Printf(s, task.Messages, err)
	if gOpts.Quiet && len(gOpts.Log) > 0 {
		fmt.Printf(s, task.Messages, err)
	}
}

func build(id int, task *Task, config string) (err error) {
	var projname = strings.Split(filepath.Base(task.MadeProj), ".")[0]
	log.Printf("mpbuild: START %s|%s (worker %d)\n", projname, config, id)
	task.Start = time.Now()
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		compilerPath := path.Join(checkWinCompiler(), "devenv.com")
		args := []string{
			task.MadeProj,
			"/build", fmt.Sprintf("%s|%s", config, "x64"),
			"/projectconfig", config,
		}

		if len(gOpts.Verbose) > 0 {
			fmt.Printf("xcodebuild %s\n", strings.Join(args, " "))
		}
		cmd = exec.Command(compilerPath, args...)
	} else {
		var target = projname + "." + config

		args := []string{
			"-project", task.MadeProj,
			"-target", target,
			"-configuration", "Default",
		}

		if GPrefs.Threads != 0 {
			args = append(args, "-jobs", fmt.Sprintf("%d", GPrefs.Threads))
		}
		if gOpts.Ios {
			args = append(args, "-arch", "arm64", "-sdk", "iphoneos")
		}
		args = append(args, "build")

		if len(gOpts.Verbose) > 0 {
			fmt.Printf("xcodebuild %s\n", strings.Join(args, " "))
		}
		cmd = exec.Command("xcodebuild", args...)
	}

	var stdoutStderr []byte
	stdoutStderr, err = cmd.CombinedOutput()
	task.Output = string(stdoutStderr[:])

	return err
}

func workerFetchTask(job *Job, config string, id int, tasks <-chan *Task, results chan<- *Task, messages chan<- string) {
	for task := range tasks {
		var err error
		err = build(id, task, config)

		task.SetCompleted()

		messages <- task.Output
		task.Output = ""

		if err != nil {
			task.Err = err
			results <- task
		} else {
			//log.Printf("Got one %d\n", j.Number)
			results <- task
		}
	}
}

func workerStdout(messages <-chan string) {
	for message := range messages {
		if !gOpts.Quiet {
			fmt.Print(message)
		}
		if len(gOpts.Log) > 0 {
			log.Print(message)
		}
	}
}

func isAloneProject(task *Task) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	for _, p := range GPrefs.Projects {
		if p.Alone && strings.Contains(task.Messages, p.Name) {
			return true
		}
	}
	return false
}

func isIgnoreProject(task *Task) bool {
	for _, p := range GPrefs.Projects {
		if p.Ignore && strings.Contains(task.Messages, p.Name) {
			return true
		}
	}
	return false
}

func run(job *Job, config string) (err error) {
	var tasks = make(chan *Task, len(job.Tasks))
	var messages = make(chan string)
	var results = make(chan *Task, len(job.Tasks))
	var cost int
	var numRunning int
	var isAloneLaunched bool

	log.Printf("._%s_.\n", config)
	fmt.Printf("._%s_.\n", config)
	go workerStdout(messages)
	for w := 1; w <= GPrefs.Workers; w++ { //runtime.NumCPU())
		go workerFetchTask(job, config, w, tasks, results, messages)
	}

	var tasksCompleted int

	for _, task := range job.Tasks {
		if task.IsCompleted() {
			tasksCompleted++
		}
	}

	for tasksCompleted < len(job.Tasks) && err == nil {
		for _, task := range job.Tasks {
			if !task.Running && !task.IsCompleted() {
				if !task.HasPendingDeps(job) {
					if (!isAloneProject(task) && !isAloneLaunched) || numRunning == 0 {
						isAloneLaunched = isAloneProject(task)
						task.Running = true
						cost += task.Cost
						numRunning++
						tasks <- task
					}
				} else {
					//fmt.Printf("Skipping %s\n", task.Messages)
				}
			}
		}

		var continueFlag = true
		for continueFlag && tasksCompleted < len(job.Tasks) {
			select {
			case task := <-results:
				tasksCompleted++
				numRunning--
				isAloneLaunched = false
				cost -= task.Cost
				if err2 := task.Err; err2 != nil {
					if err == nil && !gOpts.ContinueErr {
						err = err2
					}
					logError(task, "Error", err2)
				} else {
					var Elapsed = time.Since(task.Start).Round(time.Duration(time.Second)).String()
					log.Printf("mpbuild: ->Done %s|%s (%d/%d, cost:%d, time:%s)\n", task.Messages, config, tasksCompleted, len(job.Tasks), cost, Elapsed)
					if gOpts.Quiet && len(gOpts.Log) > 0 {
						fmt.Printf("mpbuild: ->Done %s|%s (%d/%d, cost:%d, time:%s)\n", task.Messages, config, tasksCompleted, len(job.Tasks), cost, Elapsed)
					}
				}
			case <-time.After(time.Second):
				//fmt.Fprintf(os.Stderr, "Sleeping: %d\n", tasksCompleted)
				continueFlag = false
			}
		}
	}
	close(tasks)
	close(messages)

	return err
}

var parser = flags.NewParser(&gOpts, flags.Default)

func logSetupAndDestruct() func() {
	logFile, err := os.OpenFile(gOpts.Log, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		log.Panicln(err)
	}

	log.SetOutput(logFile)

	return func() {
		e := logFile.Close()
		if e != nil {
			fmt.Fprintf(os.Stderr, "Problem closing the log file: %v\n", e)
		}
	}
}

func main() {
	//runtime.GOMAXPROCS(runtime.NumCPU())

	GPrefs.Load()

	var err error
	var args []string
	if args, err = parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			fmt.Printf("%v\n", flagsErr)
			os.Exit(1)
		}
	}

	if gOpts.UI {
		runUI()
		return
	}

	if len(gOpts.Log) > 0 {
		defer logSetupAndDestruct()()
	}

	if len(gOpts.Config) == 0 {
		gOpts.Config = []string{"Debug", "Release"}
	}

	log.Printf("Configs: %s\n", strings.Join(gOpts.Config, ","))

	for _, jobPath := range args {

		for _, config := range gOpts.Config {
			var jobFile *os.File
			if jobFile, err = os.Open(jobPath); err != nil {
				panic(err)
			}
			defer jobFile.Close()

			var job *Job
			if err = json.NewDecoder(jobFile).Decode(&job); err != nil {
				panic(err)
			}

			if job.Platform == "ios" {
				gOpts.Ios = true
			}

			if gOpts.ListPlugins {
				for _, task := range job.Tasks {
					var isPlugin = true
					for _, task2 := range job.Tasks {
						if task2.DependsOn(task.ID) && task2.ID != len(job.Tasks)-1 {
							//fmt.Printf("%s depends on %s\n", task2.Messages, task.Messages)
							isPlugin = false
							break
						}
					}
					if isPlugin && task.ID != len(job.Tasks)-1 {
						fmt.Printf("mpbuild: Plugin: %s\n", task.Messages)
					}
				}
				os.Exit(0)
			}

			// handle --start
			if len(gOpts.Start) > 0 {
				ind := job.Search(gOpts.Start)
				if ind != -1 {
					for cnt, task := range job.Tasks {
						if cnt == ind {
							break
						}
						task.SetCompleted()
					}
				}
			}

			// handle --only
			if len(gOpts.Only) > 0 {
				var allOnly = strings.Split(gOpts.Only, ",")

				for _, task := range job.Tasks {
					var isOnly bool
					for _, only := range allOnly {
						isOnly = strings.Contains(task.Messages, strings.TrimSpace(only))
						if isOnly {
							break
						}
					}
					if !isOnly {
						task.SetCompleted()
					}
				}
			}

			// handle --not
			if len(gOpts.Not) > 0 {
				var allNot = strings.Split(gOpts.Not, ",")

				for _, task := range job.Tasks {
					var isNot bool
					for _, not := range allNot {
						isNot = strings.Contains(task.Messages, strings.TrimSpace(not))
						if isNot {
							task.SetCompleted()
							break
						}
					}
				}
			}

			// handle --deps
			if len(gOpts.Deps) > 0 {
				var allDeps = strings.Split(gOpts.Deps, ",")

				var todo = make(map[int]bool)

				for _, task := range job.Tasks {
					for _, dep := range allDeps {
						if strings.Contains(task.Messages, strings.TrimSpace(dep)) {
							todo[task.ID] = true
						}
					}
				}
				var previousSize = -1
				var size = len(todo)

				for previousSize != size {
					size = len(todo)

					for _, task := range job.Tasks {
						for ID := range todo {
							if task.DependsOn(ID) {
								todo[task.ID] = true
								break
							}
						}
					}
					previousSize = len(todo)
				}

				for _, task := range job.Tasks {
					if _, ok := todo[task.ID]; !ok {
						task.SetCompleted()
					}
				}
			}

			// skip ignored projects
			for _, task := range job.Tasks {
				if isIgnoreProject(task) {
					log.Printf("Skipping ignored project '%s', based on prefs\n", task.Messages)
					if gOpts.Quiet && len(gOpts.Log) > 0 {
						fmt.Printf("Skipping ignored project '%s', based on prefs\n", task.Messages)
					}
					task.SetCompleted()
				}
			}

			if len(gOpts.Deps) > 0 || len(gOpts.Start) > 0 || len(gOpts.Not) > 0 || len(gOpts.Only) > 0 {
				for _, task := range job.Tasks {
					if !task.IsCompleted() {
						fmt.Printf("mpbuild: Will build: %v [%d]\n", task.Messages, task.ID)
					}
				}
			}

			// build!
			if err = run(job, config); err != nil {
				panic(err)
			}
		}
	}

}

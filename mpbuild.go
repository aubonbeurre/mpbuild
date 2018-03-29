package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jessevdk/go-flags"
)

var gOpts struct {
	// Slice of bool will append 'true' each time the option
	// is encountered (can be set multiple times, like -vvv)
	Verbose []bool `short:"v" long:"verbose" description:"Show verbose debug information"`
	Job     string `short:"j" long:"job" description:"Job file" required:"true"`
	Config  string `short:"c" long:"config" description:"Debug or Release" default:"Debug"`
	Log     string `short:"l" long:"log" description:"Log file"`
	Workers int    `short:"w" long:"workers" description:"Number of workers" default:"3"`
	Threads int    `short:"t" long:"threads" description:"Number of threads for xcodebuild"`
	Ios     bool   `short:"i" long:"ios" description:"ios build"`
	Quiet   bool   `short:"q" long:"quiet" description:"Suppress most xcodebuild output"`
	Start   string `short:"s" long:"start" description:"Start at project <search>"`
}

// Job ...
type Job struct {
	Tasks []*Task `json:"tasks"`
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

func build(id int, task *Task) (err error) {
	var projname = strings.Split(filepath.Base(task.MadeProj), ".")[0]
	log.Printf("START %s (worker %d)\n", projname, id)
	task.Start = time.Now()
	var cmd *exec.Cmd
	var target = projname + "." + gOpts.Config

	args := []string{
		"-project", task.MadeProj,
		"-target", target,
		"-configuration", "Default",
	}

	if gOpts.Threads != 0 {
		args = append(args, "-jobs", fmt.Sprintf("%d", gOpts.Threads))
	}
	if gOpts.Ios {
		args = append(args, "-arch", "arm64", "-sdk", "iphoneos")
	}
	args = append(args, "build")
	if len(gOpts.Verbose) > 0 {
		fmt.Printf("xcodebuild %s\n", strings.Join(args, " "))
	}

	cmd = exec.Command("xcodebuild", args...)
	var stdout io.ReadCloser
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr io.ReadCloser
	stderr, err = cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmdReader := io.MultiReader(stdout, stderr)

	err = cmd.Start()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(cmdReader)
	for scanner.Scan() {
		//fmt.Println(scanner.Text())
		txt := scanner.Text()
		if len(gOpts.Log) > 0 {
			log.Println(task.Messages + ": " + txt)
		}
		task.Output += txt + "\n"
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}

	err = cmd.Wait()
	if err != nil {
		//fmt.Println(task.Output)
		return err
	}

	return nil
}

func workerFetchTask(job *Job, id int, tasks <-chan *Task, results chan<- *Task, messages chan<- string) {
	for task := range tasks {
		var err error
		err = build(id, task)

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
	}
}

func run(job *Job) (err error) {
	var tasks = make(chan *Task, len(job.Tasks))
	var messages = make(chan string)
	var results = make(chan *Task, len(job.Tasks))
	var cost int

	go workerStdout(messages)
	for w := 1; w <= gOpts.Workers; w++ { //runtime.NumCPU())
		go workerFetchTask(job, w, tasks, results, messages)
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
					task.Running = true
					cost += task.Cost
					tasks <- task
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
				cost -= task.Cost
				if err2 := task.Err; err2 != nil {
					if err == nil {
						err = err2
					}
					log.Printf("Error %s (%v)\n", task.Messages, err)
					if gOpts.Quiet && len(gOpts.Log) > 0 {
						fmt.Printf("Error %s (%v)\n", task.Messages, err)
					}
				} else {
					var Elapsed = time.Since(task.Start).Round(time.Duration(time.Second)).String()
					log.Printf("->Done %s (%d/%d, cost:%d, time:%s)\n", task.Messages, tasksCompleted, len(job.Tasks), cost, Elapsed)
					if gOpts.Quiet && len(gOpts.Log) > 0 {
						fmt.Printf("->Done %s (%d/%d, cost:%d, time:%s)\n", task.Messages, tasksCompleted, len(job.Tasks), cost, Elapsed)
					}
				}
			default:
				time.Sleep(time.Second)
				continueFlag = false
			}
		}
	}
	close(tasks)
	close(messages)

	return nil
}

var parser = flags.NewParser(&gOpts, flags.Default)

// LogSetupAndDestruct ...
func LogSetupAndDestruct() func() {
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

	var err error
	var args []string
	if args, err = parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}

	if len(args) > 1 {
		log.Panic(fmt.Errorf("Too many or not enough arguments"))
	}

	if len(gOpts.Log) > 0 {
		defer LogSetupAndDestruct()()
	}

	var jobFile *os.File
	if jobFile, err = os.Open(gOpts.Job); err != nil {
		panic(err)
	}
	defer jobFile.Close()

	var job *Job
	if err = json.NewDecoder(jobFile).Decode(&job); err != nil {
		panic(err)
	}

	if len(gOpts.Start) > 0 {
		ind := job.Search(gOpts.Start)
		if ind != -1 {
			for cnt, task := range job.Tasks {
				if cnt == ind {
					break
				}
				task.Running = true
				task.SetCompleted()
			}
		}
	}

	if err = run(job); err != nil {
		panic(err)
	}
}

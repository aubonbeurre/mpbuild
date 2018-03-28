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
	Workers int    `short:"w" long:"workers" description:"Number of workers" default:"3"`
	Threads int    `short:"t" long:"threads" description:"Number of threads for xcodebuild"`
	Ios     bool   `short:"i" long:"ios" description:"ios build"`
}

// Job ...
type Job struct {
	Tasks []*Task `json:"tasks"`
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

func build(id int, task *Task, messages chan<- string) (err error) {
	var projname = strings.Split(filepath.Base(task.MadeProj), ".")[0]
	log.Printf("START %s (worker %d)\n", projname, id)
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
		args = append(args, "-arch", "armv7", "-sdk", "iphoneos10.2")
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
		messages <- scanner.Text()
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
		err = build(id, task, messages)

		task.SetCompleted()

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
		fmt.Println(message)
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
		for continueFlag {
			select {
			case task := <-results:
				tasksCompleted++
				cost -= task.Cost
				if err = task.Err; err != nil {
					log.Printf("Error %s (%v)\n", task.Messages, err)
				} else {
					log.Printf("->Done %s (%d/%d, cost:%d)\n", task.Messages, task.ID, len(job.Tasks), cost)
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

func main() {
	var parser = flags.NewParser(&gOpts, flags.Default)

	var err error
	var args []string
	if args, err = parser.Parse(); err != nil {
		log.Panic(err)
	}

	if len(args) > 2 {
		log.Panic(fmt.Errorf("Too many or not enough arguments"))
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

	if err = run(job); err != nil {
		panic(err)
	}
}

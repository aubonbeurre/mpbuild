package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/jroimartin/gocui"
	"github.com/shirou/gopsutil/mem"
)

var (
	done          = make(chan struct{})
	wg            sync.WaitGroup
	gRefreshCount int32
)

func layout(g *gocui.Gui) (err error) {
	maxX, maxY := g.Size()

	if _, err = g.SetView("side", -1, -1, int(0.2*float32(maxX)), maxY-15); err != nil &&
		err != gocui.ErrUnknownView {
		return err
	}
	if _, err = g.SetView("main", int(0.2*float32(maxX)), -1, maxX, maxY-15); err != nil &&
		err != gocui.ErrUnknownView {
		return err
	}
	if _, err = g.SetView("cmdline", -1, maxY-15, maxX, maxY); err != nil &&
		err != gocui.ErrUnknownView {
		return err
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	close(done)
	return gocui.ErrQuit
}

func runUI() {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	g.SetManagerFunc(layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	wg.Add(1)
	go autorefresh(g)

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}

	wg.Wait()
}

func dorefresh(g *gocui.Gui) (err error) {
	var v *gocui.View

	atomic.CompareAndSwapInt32(&gRefreshCount, 1, 0)

	v, err = g.View("side")
	if err != nil {
		return err
	}
	v.Clear()
	memory, _ := mem.VirtualMemory()

	// almost every return value is a struct
	fmt.Fprintf(v, "Total: %v\nFree: %v\nUsedPercent: %.2f%%\n", humanize.Bytes(memory.Total), humanize.Bytes(memory.Free), memory.UsedPercent)

	v, err = g.View("main")
	if err != nil {
		return err
	}
	v.Clear()
	var cmd *exec.Cmd

	args := []string{}
	cmd = exec.Command("distccmon-text", args...)

	var stdoutStderr []byte
	stdoutStderr, _ = cmd.CombinedOutput()

	fmt.Fprintln(v, "distccmon-text output:")
	fmt.Fprintln(v, "")
	fmt.Fprintln(v, string(stdoutStderr[:]))

	v, err = g.View("cmdline")
	if err != nil {
		return err
	}
	v.Clear()
	args2 := []string{
		"-A",
	}
	cmd = exec.Command("ps", args2...)

	stdoutStderr, _ = cmd.CombinedOutput()

	fmt.Fprintln(v, "ps -A output:")
	fmt.Fprintln(v, "")
	lines := strings.Split(string(stdoutStderr[:]), "\n")
	for _, l := range lines {
		if strings.Contains(l, "xcodebuild") {

			fmt.Fprintln(v, l[strings.Index(l, "xcodebuild"):])
		}
	}

	return nil
}

func autorefresh(g *gocui.Gui) {
	defer wg.Done()

	for {
		select {
		case <-done:
			return
		case <-time.After(2 * time.Second):
			if atomic.CompareAndSwapInt32(&gRefreshCount, 0, 1) {
				g.Update(dorefresh)
			}

		}
	}
}

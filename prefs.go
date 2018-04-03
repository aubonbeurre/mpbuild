package main

import (
	"io/ioutil"
	"log"
	"os/user"
	"path"

	yaml "gopkg.in/yaml.v2"
)

var (
	// GPrefs ...
	GPrefs Prefs
)

// Prefs ...
type Prefs struct {
	Workers  int `yaml:"workers"`
	Threads  int `yaml:"threads"`
	Projects []Project
}

// Project ...
type Project struct {
	Name  string `yaml:"name"`
	Alone bool   `yaml:"alone,omitempty"`
}

// Load ...
func (d *Prefs) Load() {
	// load prefs
	var usr *user.User
	var err error
	if usr, err = user.Current(); err != nil {
		panic(err)
	}
	var prefFile = path.Join(usr.HomeDir, ".mpbuild")
	var blob []byte
	if blob, err = ioutil.ReadFile(prefFile); err != nil {
		var data []byte
		GPrefs.Workers = 3
		GPrefs.Threads = 10
		GPrefs.Projects = []Project{
			{Name: "HandlerProject", Alone: true},
			{Name: "HandlerTimeline", Alone: true},
			{Name: "HandlerGraphics", Alone: true},
			{Name: "HandlerSourceMonitor", Alone: true},
			{Name: "HandlerEffectControls", Alone: true},
			{Name: "TeamProjectsLocalHub", Alone: true},
			{Name: "TeamProjectsLocalLib", Alone: true},
		}
		if data, err = yaml.Marshal(&GPrefs); err != nil {
			panic(err)
		}
		if err = ioutil.WriteFile(prefFile, data, 0600); err != nil {
			panic(err)
		}
		log.Printf("%s created\n", prefFile)
	} else {
		if err = yaml.Unmarshal(blob, &GPrefs); err != nil {
			panic(err)
		}
		log.Printf("%s loaded\n", prefFile)
	}
}

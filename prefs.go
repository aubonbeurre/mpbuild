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
	Workers int `yaml:"workers"`
	Threads int `yaml:"threads"`
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

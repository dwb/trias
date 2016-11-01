package main

import (
	"encoding/json"
	"os"
	"path"
)

type config struct {
	Profiles map[string]profile `json:"profiles"`
}

type profile struct {
	Name string `json:"-"`
	Host string `json:"host"`
	User string `json:"user,omitempty"`
	Port int    `json:"port,omitempty"`
}

const configFileName = ".config/trias.json"

func loadConfig() (conf config, err error) {
	conf.Profiles = make(map[string]profile, 0)

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		// empty config
		return
	}

	f, err := os.Open(path.Join(homeDir, configFileName))
	if err != nil {
		return conf, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	err = dec.Decode(&conf)
	return
}

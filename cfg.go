package main

import (
	"encoding/json"
	"io/ioutil"

	"github.com/pkg/errors"
)

type config struct {
	Listen    string `json:"listen"`
	LogPath   string `json:"log_path"`
	FilesPath string `json:"files_path"`
	Verbose   bool   `josn:"verbose"`
}

func ParseConfig(path string) (cfg *config, err error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.WithMessage(err, "读取配置文件错误")
	}
	cfg = &config{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.WithMessage(err, "解析配置文件错误")
	}

	if len(cfg.Listen) == 0 {
		cfg.Listen = ":80"
	}
	if len(cfg.LogPath) == 0 {
		cfg.LogPath = "./filesrv.log"
	}
	if len(cfg.FilesPath) == 0 {
		cfg.FilesPath = "./files/"
	}
	return cfg, nil
}

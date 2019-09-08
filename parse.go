package main

import (
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Stage struct {
	Dependencies []Dependency

	Files     []FilePattern
	Dir       string
	Recursive bool

	Cmd            []string
	EarlyTerminate bool
	Start          bool
}

func parseConfig() (stages map[string]Stage) {
	stages = make(map[string]Stage)

	logger := log.WithFields(log.Fields{
		"stage": "",
	})

	for stage := range viper.AllSettings() {
		logger.Data["stage"] = stage

		if stage == "dirname" || stage == "loglevel" {
			continue
		}

		if _, ok := stages[stage]; ok {
			logger.Errorln("Stage name already in use")
			return
		}

		setDefaults(stage, logger)
		stages[stage] = Stage{
			Dir:            viper.GetString(stage + ".dir"),
			Recursive:      viper.GetBool(stage + ".recursive"),
			Cmd:            viper.GetStringSlice(stage + ".cmd"),
			EarlyTerminate: viper.GetBool(stage + ".early-terminate"),
			Start:          viper.GetBool(stage + ".start"),
		}
	}

	exit := false
	for stage := range stages {
		logger.Data["stage"] = stage

		dependencies, ok := parseDependencies(stage, stages, logger)
		exit = exit || !ok

		s := stages[stage]
		s.Dependencies = append(s.Dependencies, dependencies...)
		stages[stage] = s
	}
	if exit {
		return
	}

	for stage := range stages {
		logger.Data["stage"] = stage

		files, ok := parseFiles(stage, logger)
		exit = exit || !ok

		s := stages[stage]
		s.Files = append(s.Files, files...)
		stages[stage] = s
	}
	if exit {
		return
	}

	return
}

type Dependency struct {
	Stage string
	Async bool
}

func parseDependencies(stage string, stages map[string]Stage, logger *log.Entry) (dependencies []Dependency, ok bool) {
	depends, ok1 := viper.Get(stage + ".depends").([]interface{})
	if !ok1 {
		logger.Errorln("Could not process dependency list")
		return
	}

	ok = true

	for _, dependency := range depends {
		if dependency, ok2 := parseDependency(dependency); ok2 {
			if _, _ok := stages[dependency.Stage]; !_ok {
				logger.Warnf("Dependency (%s) doesn't exist as a stage\n", dependency.Stage)
			} else {
				dependencies = append(dependencies, dependency)
			}
		} else {
			logger.Errorf("Could not process dependency value %v\n", dependency)
			ok = false
		}
	}

	return
}

func parseDependency(dep interface{}) (dependency Dependency, ok bool) {
	if _dep, _ok := dep.(string); _ok {
		return Dependency{_dep, false}, true
	} else if _dep, _ok := dep.(map[interface{}]interface{}); _ok {
		for k, v := range _dep {
			if v == nil {
				dependency.Stage, ok = k.(string)
				break
			}
		}
		if dependency.Stage == "" {
			ok = false
		} else {
			async, _ok := _dep["async"].(bool)
			dependency.Async = async && _ok
		}
	}
	return
}

type FilePattern struct {
	Pattern *regexp.Regexp
	Exclude bool
}

func parseFiles(stage string, logger *log.Entry) (files []FilePattern, ok bool) {
	_files, ok1 := viper.Get(stage + ".files").([]interface{})
	if !ok1 {
		logger.Errorln("Could not process files list")
		return
	}

	ok = true

	for _, filePattern := range _files {
		filePattern, err := parseFilePattern(filePattern)
		if err == nil {
			files = append(files, filePattern)
		} else {
			logger.Errorf("Could not process file pattern value %v\nError: %s", filePattern, err.Error())
			ok = false
		}
	}

	return
}

func parseFilePattern(filePattern interface{}) (fp FilePattern, err error) {
	if _fp, ok := filePattern.(string); ok {
		pattern, patternErr := regexp.Compile(_fp)
		if patternErr != nil {
			err = fmt.Errorf("could not compile file pattern regex:\n\t%s", patternErr.Error())
			return
		}
		fp.Pattern = pattern
	} else if _fp, ok := filePattern.(map[interface{}]interface{}); ok {
		if len(_fp) == 1 {
			for k, v := range _fp {
				pattern, patternErr := regexp.Compile(k.(string))
				if patternErr != nil {
					err = fmt.Errorf("could not compile file pattern regex:\n\t%s", patternErr.Error())
					return
				}
				fp.Pattern = pattern

				exclude, ok := v.(bool)
				fp.Exclude = exclude && ok
			}
		} else {
			for k, v := range _fp {
				if v == nil {
					pattern, patternErr := regexp.Compile(k.(string))
					if patternErr != nil {
						err = fmt.Errorf("could not compile file pattern regex:\n\t%s", patternErr.Error())
						return
					}
					fp.Pattern = pattern
					break
				}
			}
			exclude, _ok := _fp["exclude"].(bool)
			fp.Exclude = exclude && _ok
		}
	}
	return
}

func setDefaults(stage string, logger *log.Entry) {
	viper.SetDefault(stage+".recursive", true)
	viper.SetDefault(stage+".start", true)

	if !viper.IsSet(stage + ".cmd") {
		logger.Warn("No command provided")
	}
	viper.SetDefault(stage+".cmd", []string{"echo", "No command provided"})

	viper.SetDefault(stage+".dir", ".")

	files := make([]interface{}, 2)
	files[0] = ".*"
	files[1] = map[interface{}]interface{}{"^\\..*": nil, "exclude": true}
	viper.SetDefault(stage+".files", files)

	viper.SetDefault(stage+".depends", []interface{}{})

	viper.SetDefault(stage+".early-terminate", true)
}

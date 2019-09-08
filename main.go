package main

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/radovskyb/watcher"

	"github.com/fsnotify/fsnotify"

	"github.com/spf13/viper"
)

func main() {
	viper.SetEnvPrefix("MW")
	viper.SetDefault("dirname", ".")

	viper.SetConfigName("multiwatcher-config")

	viper.AddConfigPath("/etc/multiwatcher")
	viper.AddConfigPath("/home/.config/multiwatcher")
	viper.AddConfigPath("$HOME/.multiwatcher")
	viper.AddConfigPath(viper.GetString("dirname"))
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			fmt.Fprintln(os.Stderr, "Could not find config file")
		} else {
			fmt.Fprintf(os.Stderr, "There was an error reading the config file:\n%s\n", err.Error())
		}
	}

	setLogLevel := func() {
		if viper.IsSet("loglevel") {
			logLevel := viper.GetString("loglevel")
			switch logLevel {
			case "debug":
				log.SetLevel(log.DebugLevel)
			case "error":
				log.SetLevel(log.ErrorLevel)
			case "info":
				log.SetLevel(log.InfoLevel)
			case "fatal":
				log.SetLevel(log.FatalLevel)
			case "panic":
				log.SetLevel(log.PanicLevel)
			case "trace":
				log.SetLevel(log.TraceLevel)
			case "warn":
				log.SetLevel(log.WarnLevel)
			}
		}
	}

	setLogLevel()
	stages := parseConfig()

	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		setLogLevel()
		stages = parseConfig()

		log.Infoln("Updated config")
	})

	quit := make(chan struct{})

	type env struct {
		trigger string
		file    string
		stage   string
	}

	type watchData struct {
		asyncParentChans []chan env
		syncParentChans  []chan env

		childChans []chan env

		w *watcher.Watcher

		logger *log.Entry
	}

	stageWatchData := map[string]watchData{}

	for name, stage := range stages {
		wd := watchData{w: watcher.New()}

		wd.w.AddFilterHook(MultiRegexFilterHook(stage.Files))
		wd.logger = log.New().WithFields(log.Fields{
			"stage": name,
		})

		stageWatchData[name] = wd
	}

	for name, stage := range stages {
		for _, dep := range stage.Dependencies {
			s := stageWatchData[dep.Stage]
			q := make(chan env)
			if dep.Async {
				s.asyncParentChans = append(s.asyncParentChans, q)
			} else {
				s.syncParentChans = append(s.syncParentChans, q)
			}
			stageWatchData[dep.Stage] = s
			s = stageWatchData[name]
			s.childChans = append(s.childChans, q)
			stageWatchData[name] = s
		}
	}

	for name, stage := range stages {
		wd := stageWatchData[name]

		go func(stage Stage, name string, wd *watchData) {
			cases := make([]reflect.SelectCase, len(wd.childChans)+3)
			cases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(wd.w.Error)}
			cases[1] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(wd.w.Closed)}
			cases[2] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(wd.w.Event)}
			for i, ch := range wd.childChans {
				cases[i+3] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
			}

			// var p *os.Process

			initCommand := func(stage Stage, env env) *exec.Cmd {
				cmd := exec.Command(stage.Cmd[0], stage.Cmd[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				cmd.Env = append(os.Environ(),
					"MW_TRIGGER="+env.trigger,
					"MW_FILE="+env.file,
					"MW_STAGE="+env.stage,
				)

				return cmd
			}

			spawnProcess := func(cmd **exec.Cmd, stage Stage, wd *watchData, env env) {
				if *cmd != nil {
					var err error
					if stage.EarlyTerminate {
						err = (*cmd).Process.Kill()
					}
					err = (*cmd).Wait()

					*cmd = nil

					if err != nil {
						wd.logger.Errorln(err)
					}
				}

				*cmd = initCommand(stage, env)
				if err := (*cmd).Start(); err != nil {
					wd.logger.Errorf("Could not spawn process\nError: %s", err.Error())
					return
				}

				for _, c := range wd.asyncParentChans {
					c <- env
				}

				go func(cmd **exec.Cmd, wd *watchData) {
					if err := (*cmd).Wait(); err != nil {
						wd.logger.Warnf("Process had errors\nError: %s", err.Error())
					} else {
						if !(*cmd).ProcessState.Success() {
							wd.logger.Warnf("Process resulted in non 0 error code\nError: %s", (*cmd).ProcessState)
						}
					}

					*cmd = nil

					for _, c := range wd.syncParentChans {
						c <- env
					}
				}(cmd, wd)
			}

			var cmd *exec.Cmd

			if stage.Start {
				spawnProcess(&cmd, stage, wd, env{
					trigger: "",
					file:    "",
					stage:   name,
				})
			}

			for {

				chosen, value, ok := reflect.Select(cases)
				if !ok {
					break
				}

				switch chosen {
				case 0:
					wd.logger.Errorln(value.Interface().(error))
				case 1:
					return
				case 2:
					event := value.Interface().(watcher.Event)

					spawnProcess(&cmd, stage, wd, env{
						trigger: "",
						file:    event.Path,
						stage:   name,
					})
				default:
					wd.logger.Infoln("Reloading...")

					trigger := value.Interface().(env)

					spawnProcess(&cmd, stage, wd, env{
						trigger: trigger.stage,
						file:    trigger.file,
						stage:   name,
					})
				}
			}
		}(stage, name, &wd)

		go func(name string, stage Stage, wd *watchData) {
			wd.logger.Debugln(wd.logger.Data["stage"], name, stage.Dir+"/")
			if stage.Recursive {
				if err := wd.w.AddRecursive(stage.Dir); err != nil {
					wd.logger.Fatalln(err)
				}
			} else {
				if err := wd.w.Add(stage.Dir); err != nil {
					wd.logger.Fatalln(err)
				}
			}

			for path, f := range wd.w.WatchedFiles() {
				wd.logger.Infof("%s: %s\n", path, f.Name())
			}

			if err := wd.w.Start(time.Millisecond * 100); err != nil {
				wd.logger.Fatalln(err)
			}

			wd.logger.Infoln("Started watcher")
		}(name, stage, &wd)
	}

	<-quit
}

func MultiRegexFilterHook(files []FilePattern) watcher.FilterFileHookFunc {
	return func(info os.FileInfo, fullPath string) error {
		for i := len(files) - 1; i >= 0; i-- {
			if files[i].Pattern.MatchString(info.Name()) {
				if files[i].Exclude {
					return watcher.ErrSkip
				} else {
					return nil
				}
			}
		}
		return watcher.ErrSkip
	}
}

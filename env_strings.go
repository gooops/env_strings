package env_strings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
)

const (
	ENV_STRINGS_KEY = "ENV_STRINGS"
	ENV_STRINGS_EXT = ".env"

	ENV_STRINGS_CONF = "/etc/env_strings.conf"

	ENV_STRINGS_CONFIG_KEY = "ENV_STRINGS_CONF"
)

const (
	STORAGE_REDIS = "redis"
)

type EnvStringConfig struct {
	Storages []StorageConfig `json:"storages" yaml:"storages"`
}

type StorageConfig struct {
	Engine  string                 `json:"engine" yaml:"engine"`
	Options map[string]interface{} `json:"options" yaml:"options"`
}

type option func(envStrings *EnvStrings)

type EnvStrings struct {
	envName   string
	envExt    string
	tmplFuncs *TemplateFuncs

	configFile string
	configType string
	envConfig  EnvStringConfig
}

func FuncMap(name string, function interface{}) option {
	return func(e *EnvStrings) {
		e.RegisterFunc(name, function)
	}
}

func EnvStringsConfig(fileName string) option {
	return func(e *EnvStrings) {
		e.configFile = fileName
	}
}

func NewEnvStrings(envName string, envExt string, configType string, opts ...option) *EnvStrings {
	if envName == "" {
		panic("env_strings: env name could not be empty")
	}

	envStrings := &EnvStrings{
		envName:    envName,
		envExt:     envExt,
		configFile: ENV_STRINGS_CONF,
		configType: configType,
		tmplFuncs:  NewTemplateFuncs(),
	}

	if opts != nil && len(opts) > 0 {
		for _, opt := range opts {
			opt(envStrings)
		}
	}
	envStringsConf := os.Getenv(ENV_STRINGS_CONFIG_KEY)
	if envStringsConf != "" {
		envStrings.configFile = envStringsConf
	}

	if envStrings.configFile != "" {
		if err := envStrings.loadConfig(envStrings.configFile); err != nil {
			if !os.IsNotExist(err) {
				panic(err)
			} else {
				return envStrings
			}
		}

		if envStrings.envConfig.Storages != nil {
			for _, storageConf := range envStrings.envConfig.Storages {
				switch storageConf.Engine {
				case STORAGE_REDIS:
					{
						extFucnRedis := NewExtFuncsRedis(storageConf.Options)
						redisFuncs := extFucnRedis.GetFuncs()

						if redisFuncs == nil {
							panic("ext funcs of redis is nil")
						}

						for funcName, fn := range redisFuncs {
							if err := envStrings.RegisterFunc(funcName, fn); err != nil {
								panic(err)
							}
						}
					}
				default:
					{
						panic("unknown storage type")
					}
				}
			}
		}
	}
	return envStrings
}

func (p *EnvStrings) Execute(str string) (ret string, err error) {
	return p.ExecuteWith(str, nil)
}

func (p *EnvStrings) ExecuteWith(str string, envValues map[string]interface{}) (ret string, err error) {
	strConfigFiles := os.Getenv(p.envName)

	configFiles := strings.Split(strConfigFiles, ";")

	files := []string{}

	if len(configFiles) > 0 {

		for _, confFile := range configFiles {
			confFile = strings.TrimSpace(confFile)
			if confFile == "" {
				continue
			}

			var fi os.FileInfo
			if fi, err = os.Stat(confFile); err != nil {
				return
			}

			if fi.IsDir() {
				var dir *os.File
				if dir, err = os.Open(confFile); err != nil {
					return
				}

				var names []string
				if names, err = dir.Readdirnames(-1); err != nil {
					return
				}

				for _, name := range names {
					if ext := filepath.Ext(name); ext == p.envExt {
						filePath := strings.TrimRight(confFile, "/")
						files = append(files, filePath+"/"+name)
					}
				}
			} else {
				if ext := filepath.Ext(confFile); ext == p.envExt {
					files = append(files, confFile)
				}
			}
		}
	}

	envs := map[string]map[string]interface{}{}

	if len(files) > 0 {
		for _, file := range files {
			var str []byte
			if str, err = ioutil.ReadFile(file); err != nil {
				return
			}

			env := map[string]interface{}{}
			// env := make(map[interface{}]interface{})
			if p.configType == "json" {
				if err = json.Unmarshal(str, &env); err != nil {
					return
				}
			} else if p.configType == "yaml" {
				if err = yaml.Unmarshal(str, &env); err != nil {
					return
				}
			}

			envs[file] = env
		}

	}

	allEnvs := map[string]interface{}{}

	for file, env := range envs {
		for envKey, envVal := range env {
			if oldValue, exist := allEnvs[envKey]; exist {
				if oldValue != envVal {
					err = fmt.Errorf("env key of %s already exist, and value not equal, env file: %s", envKey, file)
					return
				}
			} else {
				allEnvs[envKey] = envVal
			}
		}
	}

	if envValues != nil {
		for envKey, envVal := range envValues {
			if oldValue, exist := allEnvs[envKey]; exist {
				if oldValue != envVal {
					err = fmt.Errorf("env key of %s already exist, and value not equal", envKey)
					return
				}
			} else {
				allEnvs[envKey] = envVal
			}
		}
	}

	var tpl *template.Template
	if tpl, err = template.New("tmpl:" + p.envName).Funcs(p.tmplFuncs.GetFuncMaps(p.envName)).Option("missingkey=error").Parse(str); err != nil {
		return
	}

	var buf bytes.Buffer
	if err = tpl.Execute(&buf, allEnvs); err != nil {
		return
	}
	ret = buf.String()
	return
}

func Execute(str, configtype string) (ret string, err error) {
	envStrings := NewEnvStrings(ENV_STRINGS_KEY, ENV_STRINGS_EXT, configtype)
	return envStrings.Execute(str)
}

func ExecuteWith(str string, envValues map[string]interface{}, configtype string) (ret string, err error) {
	envStrings := NewEnvStrings(ENV_STRINGS_KEY, ENV_STRINGS_EXT, configtype)
	return envStrings.ExecuteWith(str, envValues)
}

func (p *EnvStrings) RegisterFunc(name string, function interface{}) (err error) {
	return p.tmplFuncs.Register(name, function)
}

func (p *EnvStrings) FuncUsageStatic() map[string][]FuncStaticItem {
	return funcStatics
}

func (p *EnvStrings) loadConfig(fileName string) (err error) {
	if _, err = os.Stat(fileName); err != nil {
		return
	}

	var data []byte
	if data, err = ioutil.ReadFile(fileName); err != nil {
		return
	}

	conf := EnvStringConfig{}

	if err = json.Unmarshal(data, &conf); err != nil {
		return
	}

	p.envConfig = conf

	return
}

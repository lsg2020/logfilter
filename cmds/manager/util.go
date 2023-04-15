package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"github.com/lsg2020/logfilter/define"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

type ScriptParam struct {
	Type string

	// log
	ReqLogFile string
	ReqLogStr  string

	// filters
	ResFilters []string

	// records
	ReqRecordsFilter  string
	ResRecordsSummary []string
	ResRecordsLogs    []string
}
type ScriptFn func(*ScriptParam)

func LoadScript(script string) (*interp.Interpreter, error) {
	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, err
	}
	if err := i.Use(interp.Symbols); err != nil {
		return nil, err
	}
	if err := i.Use(map[string]map[string]reflect.Value{
		"logfilter/logfilter": {
			"ScriptParam": reflect.ValueOf((*ScriptParam)(nil)),
		},
	}); err != nil {
		return nil, err
	}
	_, err := i.Eval(script)
	if err != nil {
		return nil, fmt.Errorf("load script failed, %w", err)
	}

	return i, nil
}

func LoadScriptEntryFunction(i *interp.Interpreter, fnName string) (ScriptFn, error) {
	v, err := i.Eval(fnName)
	if err != nil {
		return nil, fmt.Errorf("not exists function %s, %w", fnName, err)
	}
	check, ok := v.Interface().(func(*ScriptParam))
	if !ok {
		return nil, fmt.Errorf("function %s need func(str string), %w", fnName, err)
	}
	return check, nil
}

func LoadConfig() (string, *define.Config, error) {
	buf, err := ioutil.ReadFile(*ConfigFilePath)
	if err != nil {
		return "", nil, fmt.Errorf("read config failed, %w", err)
	}
	c := &define.Config{}
	err = json.Unmarshal(buf, c)
	if err != nil {
		return "", nil, fmt.Errorf("config unmarshal failed, %s %w", string(buf), err)
	}

	err = CheckConfig(c)
	if err != nil {
		return "", nil, err
	}

	return string(buf), c, nil
}

func CheckConfig(c *define.Config) error {
	// check filter
	logTargets := make(map[string]bool)
	for _, target := range c.Targets {
		if logTargets[target.ID] {
			return fmt.Errorf("log file:%s repeat", target.ID)
		}
		logTargets[target.ID] = true

		for _, f := range target.Files {
			if f.Path == "" {
				return fmt.Errorf("log file:%s need path", target.ID)
			}
			if f.SshHost == "" || f.SshPort == 0 || (f.SshKey == "" && f.SshPwd == "") || f.SshUser == "" {
				return fmt.Errorf("log file:%s need ssh info", target.ID)
			}
		}
		for _, filterID := range target.Filters {
			if c.GetFilter(filterID) == nil {
				return fmt.Errorf("log file:%s not exists filter:%s", target.ID, filterID)
			}
		}
	}

	// check script
	for _, filter := range c.Filters {
		_, err := LoadScript(filter.Script)
		if filter.Script == "" || err != nil {
			return fmt.Errorf("filter:%s base script error, %w", filter.ID, err)
		}
		i, err := LoadScript(filter.Script)
		if err != nil {
			return fmt.Errorf("filter:%s base script error, %w", filter.ID, err)
		}
		_, err = LoadScriptEntryFunction(i, filter.EntryFunc)
		if err != nil {
			return fmt.Errorf("filter:%s base script %s error, %w", filter.ID, filter.EntryFunc, err)
		}
	}

	return nil
}

type filterData struct {
	ID        string
	Cfg       *define.ConfigFilterInfo
	EntryFunc ScriptFn
}

type HTTPAuthMiddleware struct {
	user   string
	passwd string
}

func NewHTTPAuthMiddleware(user, passwd string) *HTTPAuthMiddleware {
	return &HTTPAuthMiddleware{
		user:   user,
		passwd: passwd,
	}
}

func (authMid *HTTPAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqUser, reqPasswd, hasAuth := r.BasicAuth()
		if (authMid.user == "" && authMid.passwd == "") ||
			(hasAuth && reqUser == authMid.user && reqPasswd == authMid.passwd) {
			next.ServeHTTP(w, r)
		} else {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		}
	})
}

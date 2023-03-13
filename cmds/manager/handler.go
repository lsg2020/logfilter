package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/lsg2020/logfilter/define"
	"github.com/lsg2020/logfilter/logger"
	"github.com/tidwall/gjson"
)

func (mgr *manager) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "websocket parse form failed, %v", err)
		http.Error(w, "invalid agent id", http.StatusBadRequest)
		return
	}
	ids := r.Form["id"]
	filename := r.Form["file"]
	if len(ids) == 0 || ids[0] == "" || len(filename) == 0 || filename[0] == "" {
		mgr.logger.Log(logger.LogLevelError, "websocket invalid agent id, %v", r.Form)
		http.Error(w, "invalid agent id", http.StatusBadRequest)
		return
	}

	upgrader := websocket.Upgrader{} // use default options
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "websocket upgrade failed, %v", err)
		return
	}

	mgr.BindAgentWS(ids[0], filename[0], c)
}

func (mgr *manager) handleGrafanaSearch(w http.ResponseWriter, r *http.Request) {
	reqBuf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqStr := gjson.Get(string(reqBuf), "target").String()

	searchType := gjson.Get(reqStr, "type").String()
	searchTarget := gjson.Get(reqStr, "target").String()
	searchFilter := gjson.Get(reqStr, "filter").String()

	mgr.logger.Log(logger.LogLevelDebug, "grafana search, %#v", string(reqBuf))
	resp, err := mgr.LoadVariable(r.Context(), searchType, searchTarget, searchFilter)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load targets failed, %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resBuf, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(resBuf)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load targets write data failed, %d %v", len(resBuf), err)
	}
}

func (mgr *manager) handleGrafanaSearchVariable(w http.ResponseWriter, r *http.Request) {
	reqBuf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqStr := gjson.Get(string(reqBuf), "payload.target").String()

	searchType := gjson.Get(reqStr, "type").String()
	searchTarget := gjson.Get(reqStr, "target").String()
	searchFilter := gjson.Get(reqStr, "filter").String()

	mgr.logger.Log(logger.LogLevelDebug, "grafana variable, %#v", string(reqBuf))
	names, err := mgr.LoadVariable(r.Context(), searchType, searchTarget, searchFilter)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load targets failed, %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := make([]define.Variable, 0, len(names))
	for _, r := range names {
		resp = append(resp, define.Variable{Name: r, Value: r})
	}

	resBuf, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(resBuf)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load targets write data failed, %d %v", len(resBuf), err)
	}
}

func (mgr *manager) handleGrafanaQuery(w http.ResponseWriter, r *http.Request) {
	reqBuf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqStr := string(reqBuf)

	scopedVars := gjson.Get(reqStr, "scopedVars").Map()
	queryTargetID := gjson.Get(scopedVars["target"].String(), "text").String()
	queryFilterID := gjson.Get(scopedVars["filter"].String(), "text").String()
	querySubFilterID := gjson.Get(scopedVars["sub_filter"].String(), "text").String()
	mgr.logger.Log(logger.LogLevelDebug, "grafana query, str:%s target:%s", reqStr, queryTargetID, queryFilterID, querySubFilterID)

	tableResponse := &define.TableResponse{Type: "table"}
	tableResponse.Columns = append(tableResponse.Columns, define.TableColumn{Text: "target", Type: "string"})
	tableResponse.Columns = append(tableResponse.Columns, define.TableColumn{Text: "filter", Type: "string"})
	tableResponse.Columns = append(tableResponse.Columns, define.TableColumn{Text: "sub_filter", Type: "string"})
	tableResponse.Columns = append(tableResponse.Columns, define.TableColumn{Text: "summary", Type: "string"})
	tableResponse.Columns = append(tableResponse.Columns, define.TableColumn{Text: "message", Type: "string"})

	err = mgr.co.RunSync(r.Context(), func(ctx context.Context) (err error) {
		tableResponse.Rows, err = mgr.LoadTargetRecords(ctx, queryTargetID, queryFilterID, querySubFilterID)
		return
	}, nil)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load target data failed, %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(tableResponse.Rows) == 0 {
		tableResponse.Rows = append(tableResponse.Rows, []string{queryTargetID, queryFilterID, querySubFilterID, "empty", "empty"})
	}

	resBuf, err := json.Marshal([]*define.TableResponse{tableResponse})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(resBuf)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "grafana load targets record write data failed, %d %v", len(resBuf), err)
	}
}

func (mgr *manager) handleApiReload(w http.ResponseWriter, r *http.Request) {
	err := mgr.co.RunSync(r.Context(), func(ctx context.Context) (err error) {
		config := &define.Config{}
		err = json.Unmarshal([]byte(mgr.waitReloadConfigStr), config)
		if err != nil {
			return fmt.Errorf("json data unmarshal failed, %w", err)
		}
		err = CheckConfig(config)
		if err != nil {
			return fmt.Errorf("load config failed, %w", err)
		}
		oldConfig := mgr.configStr
		err = mgr.build(ctx, config, mgr.waitReloadConfigStr)
		if err != nil {
			return fmt.Errorf("build config failed, %w", err)
		}
		mgr.logger.Log(logger.LogLevelInfo, "api reload config:\n\n %s \n\n", oldConfig)
		err = mgr.co.Await(ctx, func(ctx context.Context) error {
			return ioutil.WriteFile(*ConfigFilePath, []byte(mgr.configStr), 0666)
		})
		if err != nil {
			return fmt.Errorf("save config failed, %w", err)
		}
		return
	}, nil)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api reload config failed, %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = w.Write([]byte("ok"))
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api reload write failed, %v", err)
	}
}

func (mgr *manager) handleApiGetConfig(w http.ResponseWriter, r *http.Request) {
	var configStr string
	err := mgr.co.RunSync(r.Context(), func(ctx context.Context) (err error) {
		configStr = mgr.configStr
		return
	}, nil)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api get config failed, %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = w.Write([]byte(configStr))
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api get config write failed, %s %v", len(configStr), err)
	}
}

func (mgr *manager) handleApiPutConfig(w http.ResponseWriter, r *http.Request) {
	reqBuf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqStr := string(reqBuf)

	err = mgr.co.RunSync(r.Context(), func(ctx context.Context) (err error) {
		mgr.waitReloadConfigStr = reqStr
		return
	}, nil)
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api put config failed, %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, err = w.Write([]byte("ok"))
	if err != nil {
		mgr.logger.Log(logger.LogLevelError, "api put config write failed, %v", err)
	}
}

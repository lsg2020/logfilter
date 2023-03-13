package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"time"

	"github.com/gorilla/websocket"
	"github.com/helloyi/go-sshclient"
	"github.com/lsg2020/goco"
	"github.com/lsg2020/logfilter/define"
	"github.com/lsg2020/logfilter/logger"
)

type client struct {
	ID     string
	config *define.Config
	logger logger.Log
	co     *co.Coroutine
	ctx    context.Context
	cancel context.CancelFunc
	mgr    *manager

	filters map[string]*filterData

	fileConns   map[string]*websocket.Conn
	fileCancels map[string]context.CancelFunc
}

func (c *client) Start(r func(error)) {
	c.logger.Log(logger.LogLevelDebug, "client start id:%s", c.ID)

	err := c.co.RunAsync(c.ctx, c.start, &co.RunOptions{Result: r})
	if err != nil {
		r(err)
		return
	}
	err = c.co.RunAsync(c.ctx, c.monitor, &co.RunOptions{RunLimitTime: -1})
	if err != nil {
		r(err)
		return
	}
}

func (c *client) Reload(config *define.Config, r func(error)) {
	err := c.co.RunAsync(c.ctx, func(ctx context.Context) error {
		return c.reload(config)
	}, &co.RunOptions{Result: r})
	if err != nil {
		r(err)
	}
}

func (c *client) Close() {
	c.logger.Log(logger.LogLevelDebug, "client close id:%s", c.ID)

	c.co.Close()
	c.cancel()
}

func (c *client) BindAgentWS(conn *websocket.Conn, filename string, r func(error)) {
	err := c.co.RunAsync(c.ctx, func(ctx context.Context) error {
		if c.fileCancels[filename] != nil {
			c.fileCancels[filename]()
		}

		ctx, cancel := context.WithCancel(c.ctx)
		c.fileCancels[filename] = cancel
		c.fileConns[filename] = conn
		go c.receiver(ctx, filename, conn)
		return nil
	}, &co.RunOptions{Result: r})
	if err != nil {
		r(err)
	}
}

func (c *client) receiver(ctx context.Context, filename string, conn *websocket.Conn) {
	defer func() {
		c.logger.Log(logger.LogLevelDebug, "client finish receiver %v %v %v", c.ID, filename, conn.RemoteAddr().String())
		_ = conn.Close()
		_ = c.co.RunAsync(ctx, func(ctx context.Context) error {
			if c.fileConns[filename] == conn {
				c.fileConns[filename] = nil
				c.fileCancels[filename]()
				c.fileCancels[filename] = nil
			}
			return nil
		}, nil)
	}()

	c.logger.Log(logger.LogLevelDebug, "client start receiver %v %v %v", c.ID, filename, conn.RemoteAddr().String())
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second * 10)); err != nil {
			c.logger.Log(logger.LogLevelDebug, "client receiver set read dead line failed %v %v %v %v", c.ID, filename, conn.RemoteAddr().String(), err)
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			c.logger.Log(logger.LogLevelDebug, "client receiver read message failed %v %v %v %v", c.ID, filename, conn.RemoteAddr().String(), err)
			return
		}

		var lines []string
		err = json.Unmarshal(message, &lines)
		if err != nil {
			c.logger.Log(logger.LogLevelDebug, "client receiver read message unmarshal failed %v %v %v %v", c.ID, filename, conn.RemoteAddr().String(), err)
			return
		}

		_ = c.co.RunAsync(ctx, func(ctx context.Context) error {
			for _, line := range lines {
				c.filterLogger(filename, line)
			}
			return nil
		}, nil)

		select {
		case <-ctx.Done():
			c.logger.Log(logger.LogLevelDebug, "client receiver ctx done %v %v %v %v", c.ID, filename, conn.RemoteAddr().String(), ctx.Err())
			return
		default:
		}
	}

}

func (c *client) monitor(ctx context.Context) error {
	startRemoteAgent := func(ctx context.Context, config *define.ConfigLogFileInfo) error {
		var sshClient *sshclient.Client
		var err error
		if config.SshPwd != "" {
			sshClient, err = sshclient.DialWithPasswd(fmt.Sprintf("%s:%d", config.SshHost, config.SshPort), config.SshUser, config.SshPwd)
		} else {
			tokenFile := fmt.Sprintf("ssh_token_%s_%s", c.ID, config.Name)
			err = c.co.Await(ctx, func(ctx context.Context) error {
				return ioutil.WriteFile(tokenFile, []byte(config.SshKey), 0600)
			})
			if err != nil {
				c.logger.Log(logger.LogLevelError, "write ssh token failed, id:%s %v %v", c.ID, config.Name, err)
				return fmt.Errorf("write ssh token failed %w", err)
			}
			sshClient, err = sshclient.DialWithKey(fmt.Sprintf("%s:%d", config.SshHost, config.SshPort), config.SshUser, tokenFile)
		}
		if err != nil {
			return fmt.Errorf("ssh dial failed, %w", err)
		}

		params := &define.AgentParams{
			WebSocketAddr: fmt.Sprintf("ws://%s:%d/agentws?id=%s&file=%s", c.config.Address, c.config.Port, c.ID, config.Name),
			LogPath:       config.Path,
		}
		strParams, err := params.ToString()
		if err != nil {
			return fmt.Errorf("build agent params failed, %w", err)
		}

		agentDownloadUrl := fmt.Sprintf("http://%s:%d/static/LogFilterAgent", c.config.Address, c.config.Port)
		cmdDownload := fmt.Sprintf("curl -u %s:%s -o LogFilterAgent %s && chmod +x LogFilterAgent", c.config.AdminUser, c.config.AdminPwd, agentDownloadUrl)
		cmdRun := fmt.Sprintf("./LogFilterAgent %s", strParams)

		c.logger.Log(logger.LogLevelDebug, "client start ssh agent id:%s file:%s cmd:%s\n%s", c.ID, config.Name, cmdDownload, cmdRun)

		err = c.co.Await(ctx, func(ctx context.Context) error {
			defer sshClient.Close()
			_ = sshClient.Script(cmdDownload).Run()
			err = sshClient.Script(cmdRun).SetStdio(c, c).Run()
			if err != nil {
				return err
			}
			return nil
		})
		return nil
	}

	for {
		cfg := c.config.GetTarget(c.ID)
		if cfg == nil {
			c.logger.Log(logger.LogLevelError, "client monitor config not found id:%s", c.ID)
			goto sleep
		}

		for _, file := range cfg.Files {
			config := file
			if !cfg.Open || c.fileConns[config.Name] != nil {
				continue
			}

			_ = c.co.RunAsync(c.ctx, func(ctx context.Context) error {
				c.logger.Log(logger.LogLevelDebug, "client start ssh remote agent id:%s file:%s", c.ID, config.Name)
				err := startRemoteAgent(ctx, config)
				c.logger.Log(logger.LogLevelDebug, "client start ssh remote agent finish id:%s file:%s %v", c.ID, config.Name, err)
				return nil
			}, nil)
		}

	sleep:
		c.co.Sleep(ctx, defaultReloadConfig)
	}
}

func (c *client) Write(p []byte) (n int, err error) {
	c.logger.Log(logger.LogLevelDebug, "=====> agent output:%s", string(p))
	return len(p), nil
}

func (c *client) start(ctx context.Context) error {
	c.filters = make(map[string]*filterData)

	err := c.build(c.config)
	if err != nil {
		c.mgr.FreeClient(c)
		c.logger.Log(logger.LogLevelError, "client start failed, client_id:%s %v", c.ID, err)
		return err
	}
	return nil
}

func (c *client) reload(config *define.Config) error {
	err := c.build(config)
	if err != nil {
		c.logger.Log(logger.LogLevelError, "client start failed, client_id:%s %v", c.ID, err)
		return err
	}

	cfg := config.GetTarget(c.ID)
	for name, cancel := range c.fileCancels {
		if cancel != nil && (cfg == nil || !cfg.Open || config.GetTargetFile(c.ID, name) == nil) {
			cancel()
		}
	}
	return nil
}

func (c *client) build(config *define.Config) error {
	cfg := config.GetTarget(c.ID)
	if cfg == nil {
		return fmt.Errorf("client config not found")
	}

	filters := make(map[string]*filterData)
	for _, filterID := range cfg.Filters {
		cfgFilter := config.GetFilter(filterID)
		if cfgFilter == nil {
			return fmt.Errorf("client config filter not found id:%s", filterID)
		}
		i, err := LoadScript(cfgFilter.Script)
		if err != nil {
			return fmt.Errorf("client config load base script failed, id:%s, %w", filterID, err)
		}
		f, err := LoadScriptCheckFunction(i, cfgFilter.CheckFunctionName)
		if err != nil {
			return fmt.Errorf("client config load base script failed, id:%s, %w", filterID, err)
		}

		subs := make([]*subFilterRecordData, 0, len(cfgFilter.SubFilters))
		for _, sub := range cfgFilter.SubFilters {
			f, err := LoadScriptCheckFunction(i, sub.CheckFunctionName)
			if err != nil {
				return fmt.Errorf("client config load sub script failed, id:%s sub:%s, %w", filterID, sub.ID, err)
			}
			var summaryF ScriptSummaryFn
			if sub.SummaryFunctionName != "" {
				summaryF, err = LoadScriptSummaryFunction(i, sub.SummaryFunctionName)
				if err != nil {
					return fmt.Errorf("client config load sub script failed, id:%s sub:%s, %w", filterID, sub.ID, err)
				}
			}

			d := &subFilterRecordData{
				ID:        sub.ID,
				FilterFn:  f,
				SummaryFn: summaryF,
				Limit:     sub.Amount,
			}
			if d.Limit == 0 {
				d.Limit = defaultRecordAmount
			}
			old := c.getSubFilterRecordData(filterID, sub.ID)
			if old != nil {
				d = old
				d.FilterFn = f
				d.SummaryFn = summaryF
				d.Limit = sub.Amount
			}

			subs = append(subs, d)
		}

		filters[filterID] = &filterData{
			ID:         filterID,
			Cfg:        cfgFilter,
			FilterFn:   f,
			SubFilters: subs,
		}
	}

	c.config = config
	c.filters = filters
	return nil
}

func (c *client) LoadFilters() ([]string, error) {
	filters := make([]*filterData, 0, len(c.filters))
	for _, f := range c.filters {
		filters = append(filters, f)
	}
	sort.Slice(filters, func(i, j int) bool { return filters[i].ID > filters[j].ID })

	res := make([]string, 0, len(c.filters))
	for _, f := range filters {
		res = append(res, f.ID)
	}
	return res, nil
}

func (c *client) LoadTargetSubFilter(searchFilter string) ([]string, error) {
	f := c.getFilterData(searchFilter)
	if f == nil {
		return nil, fmt.Errorf("filter not found, client:%s filter:%s", c.ID, searchFilter)
	}

	res := make([]string, 0, 128)
	for _, sub := range f.SubFilters {
		res = append(res, sub.ID)
	}
	return res, nil
}

func (c *client) getFilterData(id string) *filterData {
	return c.filters[id]
}

func (c *client) getSubFilterRecordData(id string, subID string) *subFilterRecordData {
	filters, ok := c.filters[id]
	if !ok {
		return nil
	}
	for _, sub := range filters.SubFilters {
		if sub.ID == subID {
			return sub
		}
	}
	return nil
}

func (c *client) filterLogger(file string, line string) {
	if len(line) == 0 {
		return
	}

	record := func(data *subFilterRecordData, str string, summary string) {
		data.Files = append(data.Files, file)
		data.Lines = append(data.Lines, str)
		data.Summarys = append(data.Summarys, summary)
		if len(data.Files) > data.Limit {
			data.Files = data.Files[len(data.Files)-data.Limit:]
		}
		if len(data.Lines) > data.Limit {
			data.Lines = data.Lines[len(data.Lines)-data.Limit:]
		}
		if len(data.Summarys) > data.Limit {
			data.Summarys = data.Summarys[len(data.Summarys)-data.Limit:]
		}
	}

	for _, filter := range c.filters {
		baseOK, _, _, err := filter.FilterFn(line)
		if err != nil {
			c.logger.Log(logger.LogLevelError, "client base filter error, id:%s line:%s %v", filter.ID, line, err)
			continue
		}
		if !baseOK {
			continue
		}

		for _, sub := range filter.SubFilters {
			ok, summary, ignore, err := sub.FilterFn(line)
			if err != nil {
				record(sub, line, "filter err:"+err.Error())
				continue
			}
			if !ok {
				continue
			}
			sub.TotalAmount++
			if ignore {
				sub.IgnoreAmount++
				continue
			}
			sub.PrintAmount++
			record(sub, line, summary)
		}
	}
}

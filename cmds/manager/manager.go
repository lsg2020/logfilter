package main

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"

	"github.com/gorilla/websocket"
	"github.com/lsg2020/goco"
	"github.com/lsg2020/logfilter/define"
	"github.com/lsg2020/logfilter/logger"
)

func newManager(configStr string, config *define.Config, logger logger.Log) (*manager, error) {
	mgr := &manager{
		configStr: configStr,
		config:    config,
		logger:    logger,
		clients:   make(map[string]*client),
	}
	err := mgr.init()
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

type manager struct {
	config              *define.Config
	configStr           string
	waitReloadConfigStr string

	co     *co.Coroutine
	logger logger.Log
	ctx    context.Context
	cancel context.CancelFunc

	clients map[string]*client
}

func (mgr *manager) init() error {
	ex, err := co.NewExecuter(context.Background(), &co.ExOptions{Name: "manager"})
	if err != nil {
		return fmt.Errorf("create manager executer failed, %w", err)
	}
	coroutine, err := co.New(&co.Options{Name: "manager", Executer: ex, OnTaskRecover: func(co *co.Coroutine, t co.Task, err error) {
		mgr.logger.Log(logger.LogLevelError, "manager task recover %v\n%v", err, string(debug.Stack()))
	}})
	if err != nil {
		return fmt.Errorf("create manager coroutine failed, %w", err)
	}
	mgr.co = coroutine
	mgr.ctx, mgr.cancel = context.WithCancel(mgr.co.GetExecuter().GetCtx())

	mgr.logger.Log(logger.LogLevelInfo, "manager init, %#v", mgr.config)

	err = mgr.co.RunSync(mgr.ctx, func(ctx context.Context) error {
		return mgr.build(ctx, mgr.config, mgr.configStr)
	}, nil)
	if err != nil {
		return fmt.Errorf("manager build failed, %w", err)
	}

	mgr.logger.Log(logger.LogLevelDebug, "manager init finish")
	return nil
}

func (mgr *manager) getClient(id string) *client {
	return mgr.clients[id]
}

func (mgr *manager) build(ctx context.Context, config *define.Config, configStr string) error {
	for _, target := range config.Targets {
		c := mgr.getClient(target.ID)
		if c == nil {
			mgr.logger.Log(logger.LogLevelDebug, "manager start create client id:%s", target.ID)

			// create
			c, err := mgr.newClient(ctx, target, config)
			if err != nil {
				return fmt.Errorf("start client failed id:%s, %w", target.ID, err)
			}

			mgr.logger.Log(logger.LogLevelDebug, "manager finish create client id:%s", target.ID)
			mgr.clients[target.ID] = c
		} else {
			mgr.logger.Log(logger.LogLevelDebug, "manager start reload client id:%s", target.ID)
			err := mgr.reloadClient(ctx, c, config)
			if err != nil {
				return fmt.Errorf("client reload failed id:%s, %v", c.ID, err)
			}
		}
	}
	for _, c := range mgr.clients {
		if config.GetTarget(c.ID) == nil {
			mgr.logger.Log(logger.LogLevelDebug, "manager free client id:%s", c.ID)
			mgr.freeClient(c)
		}
	}

	mgr.config = config
	mgr.configStr = configStr
	return nil
}

func (mgr *manager) FreeClient(c *client) {
	mgr.logger.Log(logger.LogLevelDebug, "manager on free client id:%s", c.ID)
	_ = mgr.co.RunAsync(mgr.ctx, func(ctx context.Context) error {
		mgr.freeClient(c)
		return nil
	}, nil)
}

func (mgr *manager) freeClient(c *client) {
	mgr.logger.Log(logger.LogLevelDebug, "manager free client finish id:%s", c.ID)
	c.Close()
	delete(mgr.clients, c.ID)
}

func (mgr *manager) reloadClient(ctx context.Context, c *client, config *define.Config) error {
	if config.GetTarget(c.ID) == nil {
		return nil
	}

	sessionID := mgr.co.PrepareWait()
	c.Reload(config, func(err error) {
		mgr.co.Wakeup(sessionID, err)
	})
	err := mgr.co.Wait(ctx, sessionID)
	if err != nil {
		return err
	}
	return nil
}

func (mgr *manager) newClient(ctx context.Context, cfgTarget *define.ConfigTarget, config *define.Config) (*client, error) {
	ex, err := co.NewExecuter(context.Background(), &co.ExOptions{Name: "client"})
	if err != nil {
		return nil, fmt.Errorf("create client executer failed, %w", err)
	}
	coroutine, err := co.New(&co.Options{Name: "client", DebugInfo: cfgTarget.ID, Executer: ex, OnTaskRecover: func(co *co.Coroutine, t co.Task, err error) {
		mgr.logger.Log(logger.LogLevelError, "agent task recover %v\n%v", err, string(debug.Stack()))
	}})
	if err != nil {
		return nil, fmt.Errorf("create client coroutine failed, %w", err)
	}

	cCtx, cCancel := context.WithCancel(ex.GetCtx())
	c := &client{
		ID:     cfgTarget.ID,
		config: config,
		logger: mgr.logger,
		co:     coroutine,
		ctx:    cCtx,
		cancel: cCancel,
		mgr:    mgr,

		fileConns:   make(map[string]*websocket.Conn),
		fileCancels: make(map[string]context.CancelFunc),
	}
	sessionID := mgr.co.PrepareWait()
	c.Start(func(err error) { mgr.co.Wakeup(sessionID, err) })
	err = mgr.co.Wait(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return c, err
}

func (mgr *manager) BindAgentWS(id string, file string, c *websocket.Conn) {
	mgr.logger.Log(logger.LogLevelDebug, "manager websocket start bind, %v %v %v", id, file, c.RemoteAddr().String())

	err := mgr.co.RunAsync(mgr.ctx, func(ctx context.Context) (err error) {
		defer func() {
			if err != nil {
				_ = c.Close()
			}
		}()

		client := mgr.getClient(id)
		if client == nil {
			err = fmt.Errorf("client not found")
			return
		}

		sessionID := mgr.co.PrepareWait()
		client.BindAgentWS(c, file, func(err error) { mgr.co.Wakeup(sessionID, err) })
		err = mgr.co.Wait(ctx, sessionID)
		return
	}, nil)
	mgr.logger.Log(logger.LogLevelDebug, "websocket finish bind, %v %v %v %v", id, file, c.RemoteAddr().String(), err)
}

func (mgr *manager) LoadVariable(ctx context.Context, t string, target string, filter string) ([]string, error) {
	var names []string
	err := mgr.co.RunSync(ctx, func(ctx context.Context) (err error) {
		if t == "" || t == "target" {
			names, err = mgr.loadVariableTarget(ctx)
		} else if t == "filter" {
			names, err = mgr.loadVariableFilter(ctx, target)
		} else if t == "sub_filter" {
			names, err = mgr.loadVariableSubFilter(ctx, target, filter)
		}
		return
	}, nil)
	return names, err
}

func (mgr *manager) loadVariableTarget(_ context.Context) ([]string, error) {
	clients := make([]*client, 0, len(mgr.clients))
	for _, c := range mgr.clients {
		clients = append(clients, c)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].ID > clients[j].ID })

	res := make([]string, 0, 128)
	for _, c := range clients {
		cfg := mgr.config.GetTarget(c.ID)
		if cfg != nil && cfg.Open {
			res = append(res, c.ID)
		}
	}
	return res, nil
}

func (mgr *manager) loadVariableFilter(ctx context.Context, searchTarget string) ([]string, error) {
	c := mgr.getClient(searchTarget)
	if c == nil {
		return nil, fmt.Errorf("target not found: %s", searchTarget)
	}

	var res []string
	sessionID := mgr.co.PrepareWait()
	c.co.RunAsync(c.ctx, func(ctx context.Context) (err error) {
		res, err = c.LoadFilters()
		return
	}, &co.RunOptions{Result: func(err error) {
		mgr.co.Wakeup(sessionID, err)
	}})
	err := mgr.co.Wait(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load target filter failed, client:%s %w", c.ID, err)
	}
	return res, nil
}

func (mgr *manager) loadVariableSubFilter(ctx context.Context, searchTarget string, searchFilter string) ([]string, error) {
	c := mgr.getClient(searchTarget)
	if c == nil {
		return nil, fmt.Errorf("target not found: %s", searchTarget)
	}

	var res []string
	sessionID := mgr.co.PrepareWait()
	c.co.RunAsync(c.ctx, func(ctx context.Context) (err error) {
		res, err = c.LoadTargetSubFilter(searchFilter)
		return
	}, &co.RunOptions{Result: func(err error) {
		mgr.co.Wakeup(sessionID, err)
	}})
	err := mgr.co.Wait(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load targets sub filter failed, client:%s %w", c.ID, err)
	}
	return res, nil
}

func (mgr *manager) LoadTargetRecords(ctx context.Context, targetID string, filterID string, subFilterID string) ([][]string, error) {
	clientID := targetID
	client := mgr.getClient(clientID)
	if client == nil {
		return nil, fmt.Errorf("client not found target:%s", targetID)
	}

	var res [][]string
	sessionID := mgr.co.PrepareWait()
	client.co.RunAsync(client.ctx, func(ctx context.Context) error {
		filter := client.getFilterData(filterID)
		if filter == nil {
			return fmt.Errorf("filter not found:%s %s", filterID, subFilterID)
		}

		param := &ScriptParam{Type: "records", ReqRecordsFilter: subFilterID}
		filter.EntryFunc(param)
		for i := 0; i < len(param.ResRecordsLogs); i++ {
			summary := ""
			if i < len(param.ResRecordsSummary) {
				summary = param.ResRecordsSummary[i]
			}
			res = append(res, []string{clientID, filterID, subFilterID, summary, param.ResRecordsLogs[i]})
		}
		return nil
	}, &co.RunOptions{Result: func(err error) {
		mgr.co.Wakeup(sessionID, err)
	}})

	err := mgr.co.Wait(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load target records failed, target:%s %w", targetID, err)
	}
	return res, nil
}

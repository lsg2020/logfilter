package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gorilla/mux"
	"github.com/lsg2020/logfilter/logger"
)

const (
	defaultReloadConfig = time.Second * 60
)

var (
	ConfigFilePath = flag.String("config", "config.json", "")
)

//go:embed static/*
var staticFileSystem embed.FS

func main() {
	flag.Parse()

	l, err := logger.NewLogger("log filter manager--->", logger.LogLevelDebug)
	if err != nil {
		log.Fatalln("init logger failed", err)
	}

	configStr, config, err := LoadConfig()
	if err != nil {
		l.Log(logger.LogLevelError, "load config failed, %v", err)
		return
	}

	mgr, err := newManager(configStr, config, l)
	if err != nil {
		l.Log(logger.LogLevelError, "create manager failed, %v", err)
		return
	}

	router := mux.NewRouter()

	router.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)
	router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	router.HandleFunc("/debug/pprof/profile", pprof.Profile)
	router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	router.HandleFunc("/debug/pprof/trace", pprof.Trace)

	router.HandleFunc("/agentws", mgr.handleAgentWS)
	router.HandleFunc("/query", mgr.handleGrafanaQuery).Methods("POST", "GET")
	router.HandleFunc("/search", mgr.handleGrafanaSearch).Methods("POST", "GET")
	router.HandleFunc("/variable", mgr.handleGrafanaSearchVariable).Methods("POST", "GET")

	subRouter := router.NewRoute().Subrouter()
	subRouter.Use(NewHTTPAuthMiddleware(config.AdminUser, config.AdminPwd).Middleware)
	subRouter.HandleFunc("/api/reload", mgr.handleApiReload).Methods("GET")
	subRouter.HandleFunc("/api/config", mgr.handleApiGetConfig).Methods("GET")
	subRouter.HandleFunc("/api/config", mgr.handleApiPutConfig).Methods("PUT")

	// view
	staticFS, err := fs.Sub(staticFileSystem, "static")
	if err != nil {
		log.Fatalln(err)
	}
	fileSystem := http.FS(staticFS)
	subRouter.Handle("/favicon.ico", http.FileServer(fileSystem))
	subRouter.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(fileSystem)))
	subRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/", http.StatusMovedPermanently)
	})

	// start http serve
	server := http.Server{Handler: router, Addr: fmt.Sprintf("%s:%d", config.Address, config.Port)}
	err = server.ListenAndServe()
	if err != nil {
		log.Fatalln("http start failed", err)
	}
}

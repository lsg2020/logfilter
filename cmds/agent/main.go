package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lsg2020/logfilter/define"
	"github.com/lsg2020/logfilter/logger"
	"github.com/nxadm/tail"
)

func main() {
	l, err := logger.NewLogger("log filter agent--->", logger.LogLevelDebug)
	if err != nil {
		log.Fatalln("init logger failed", err)
	}
	if len(os.Args) <= 1 {
		log.Fatalln("need input params")
	}

	l.Log(logger.LogLevelInfo, "start agent %v", os.Args)
	params := &define.AgentParams{}
	err = params.FromString(os.Args[1])
	if err != nil {
		l.Log(logger.LogLevelError, "parse param failed, %v", err)
		return
	}
	l.Log(logger.LogLevelInfo, "start agent %v", params)

	l.Log(logger.LogLevelInfo, "start tail")
	tail, err := tail.TailFile(params.LogPath, tail.Config{
		ReOpen:    true,
		Follow:    true,
		Location:  &tail.SeekInfo{Offset: 0, Whence: 2},
		MustExist: false,
		Poll:      true,
	})
	if err != nil {
		l.Log(logger.LogLevelError, "open log file failed, %v", err)
		return
	}

	l.Log(logger.LogLevelInfo, "start connect websocket")
	connCtx, connCancel := context.WithTimeout(context.Background(), time.Second*5)
	defer connCancel()
	conn, _, err := websocket.DefaultDialer.DialContext(connCtx, params.WebSocketAddr, nil)
	if err != nil {
		l.Log(logger.LogLevelError, "websocket connect error, %v", err)
		return
	}
	defer conn.Close()

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	sendChannel := make(chan string, 2048)

	wg.Add(1)
	go func() {
		defer func() {
			cancel()
			wg.Done()
		}()
		for {
			select {
			case line := <-tail.Lines:
				if line.Err != nil {
					l.Log(logger.LogLevelError, "read log file failed, %v", line.Err)
					continue
				}
				str := strings.TrimSpace(line.Text)
				if len(str) == 0 {
					continue
				}

				sendChannel <- str
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer func() {
			cancel()
			wg.Done()
		}()

		lines := make([]string, 0, 1024)
		ticker := time.NewTicker(time.Second * 5)

		send := func(limitLine int) error {
			if len(lines) < limitLine {
				return nil
			}

			pack, err := json.Marshal(lines)
			if err != nil {
				return fmt.Errorf("marshal pack failed, %v", err)
			}

			if err := conn.SetWriteDeadline(time.Now().Add(time.Second * 10)); err != nil {
				return fmt.Errorf("set write dead line failed, %v", err)
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, pack); err != nil {
				return fmt.Errorf("write message failed, %v", err)
			}

			lines = lines[:0]
			return nil
		}

		for {
			select {
			case line := <-sendChannel:
				lines = append(lines, line)
				if err := send(1024); err != nil {
					l.Log(logger.LogLevelError, "send pack failed, %v", err)
					return
				}
			case <-ticker.C:
				if err := send(0); err != nil {
					l.Log(logger.LogLevelError, "send pack failed, %v", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	l.Log(logger.LogLevelInfo, "finish")
}

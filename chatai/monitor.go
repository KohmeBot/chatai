package chatai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kohmebot/chatai/chatai/model"
	"github.com/kohmebot/chatai/chatai/monitor"
	"github.com/sirupsen/logrus"
)

type namedModel struct {
	model.LargeModel
	name string
}

func (m namedModel) ModelName() string { return m.name }

func (c *ChatPlugin) startMonitor() error {
	if !c.conf.Monitor.Enable {
		return nil
	}
	if strings.TrimSpace(string(c.conf.Monitor.AccessToken)) == "" {
		return errors.New("monitor.access_token is required when monitor is enabled")
	}
	address := c.conf.Monitor.Listen
	if address == "" {
		address = "127.0.0.1:9086"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("start agent monitor: %w", err)
	}
	c.monitor = monitor.New(c.conf.Monitor.MaxRuns)
	c.monitorServer = &http.Server{Handler: c.monitor.Handler(string(c.conf.Monitor.AccessToken)), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() {
		if err := c.monitorServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("[ChatAI][Monitor] %v", err)
		}
	}()
	logrus.Infof("[ChatAI][Monitor] listening on %s", listener.Addr())
	return nil
}

// Close releases the optional monitor listener for hosts that support unloading.
func (c *ChatPlugin) Close() error {
	if c.monitorServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// SSE connections may live indefinitely; force close after the grace period.
	if err := c.monitorServer.Shutdown(ctx); err != nil {
		return c.monitorServer.Close()
	}
	return nil
}

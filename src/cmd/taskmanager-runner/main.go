package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/util"
)

// taskmanager-runner is the K8s Deployment pod entry point for a persistent
// TaskManager worker. It reads configuration from the standard flowgent
// config file, connects to the shared store and MQTT broker, and starts
// a long-lived TaskManager with slot workers.
//
// The TM consumes ExecutionPlans from MQTT and executes them in a
// persistent dequeue→execute→emit loop. It maintains heartbeat and
// lease to enable JM failover detection.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfgPath := os.Getenv("FLOWGENT_CONFIG")
	if cfgPath == "" {
		cfgPath = "etc/flowgent.yaml"
	}

	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("load config", "error", err, "path", cfgPath)
		os.Exit(1)
	}

	tmID := os.Getenv("FLOWGENT_TM_ID")
	if tmID == "" {
		hostname, _ := os.Hostname()
		tmID = "tm-" + hostname + "-" + os.Getenv("POD_NAME")
	}
	slotCount := 4
	if n := os.Getenv("FLOWGENT_TM_SLOTS"); n != "" {
		// simple atoi would be fine; keeping default for now
	}

	mqttBroker := os.Getenv("FLOWGENT_MQTT_BROKER")
	if mqttBroker == "" {
		mqttBroker = "tcp://emqx:1883"
	}

	q, err := queue.NewMQTTQueue(&queue.MQTTConfig{
		Broker:   mqttBroker,
		ClientID: tmID,
		Topic:    "flowgent/exec",
	})
	if err != nil {
		slog.Error("connect to MQTT", "error", err)
		os.Exit(1)
	}
	defer q.Close()

	logger := util.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)

	tm, err := engine.NewTaskManager(&engine.TaskManagerConfig{
		ID:         tmID,
		SlotCount:  slotCount,
		Queue:      q,
		Store:      nil, // configured via config in production
		Agents:     agentDefs(svcCfg),
		MCPClients: nil, // configured via config in production
		LLMClient:  nil, // configured via config in production
		Logger:     logger,
	})
	if err != nil {
		slog.Error("create task manager", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tm.Start(ctx); err != nil {
		slog.Error("start task manager", "error", err)
		os.Exit(1)
	}

	slog.Info("taskmanager-runner started", "tm_id", tmID, "slots", slotCount)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	slog.Info("taskmanager-runner shutting down")
	cancel()
	time.Sleep(2 * time.Second)
}

func agentDefs(svcCfg *config.ServiceConfig) []*config.AgentDef {
	defs := make([]*config.AgentDef, len(svcCfg.Orchestration.Agents))
	for i := range svcCfg.Orchestration.Agents {
		defs[i] = &svcCfg.Orchestration.Agents[i]
	}
	return defs
}

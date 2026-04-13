package rtcbench

import (
	"context"

	"github.com/rtcbench/rtcbench/pkg/log"
)

const DefaultScenarioID = "default"

type Scenario interface {
	Run(ctx context.Context, env ScenarioEnv) error
}

type ScenarioEnv interface {
	Config() *Config
	Client() *Client
	Log() *log.Logger
}

type scenarioEnv struct {
	config *Config
	client *Client
	log    *log.Logger
}

func (e *scenarioEnv) Config() *Config {
	return e.config
}

func (e *scenarioEnv) Client() *Client {
	return e.client
}

func (e *scenarioEnv) Log() *log.Logger {
	return e.log
}

type DefaultScenario struct{}

func (DefaultScenario) Run(ctx context.Context, env ScenarioEnv) error {
	return env.Client().joinAllRooms(ctx)
}

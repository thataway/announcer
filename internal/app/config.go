package app

import (
	"github.com/thataway/announcer/internal/config"
)

/*//Sample of config
logger:
  level: INFO

trace:
  enable: true

metrics:
  enable: true

server:
  endpoint: tcp://127.0.0.1:9001
  graceful-shutdown: 30s

services:
  announce:
    interface-name: ep1
*/

const (
	//LoggerLevel ...
	LoggerLevel = config.ValueString("logger/level")

	//ServerEndpoint ...
	ServerEndpoint = config.ValueString("server/endpoint")
	//ServerGracefulShutdown ...
	ServerGracefulShutdown = config.ValueDuration("server/graceful-shutdown")

	//MetricsEnable ...
	MetricsEnable = config.ValueBool("metrics/enable")

	//TraceEnable ...
	TraceEnable = config.ValueBool("trace/enable")

	//AnnounceInterfaceName net interface name
	AnnounceInterfaceName = config.ValueString("services/announce/interface-name")
)

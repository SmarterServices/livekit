// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"net/http"

	"github.com/rs/cors"
	"github.com/twitchtv/twirp"
	"github.com/urfave/negroni/v3"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/protocol/utils/xtwirp"

	"github.com/livekit/livekit-server/pkg/config"
	"github.com/livekit/livekit-server/pkg/routing"
)

// NewGatewayLivekitServer creates a minimal LiveKit server for gateway-only mode
// Only runs egress orchestration services, no RTC/media/room services
func NewGatewayLivekitServer(
	conf *config.Config,
	egressService *EgressService,
	ioService *IOInfoService,
	keyProvider auth.KeyProvider,
	currentNode routing.LocalNode,
) (s *LivekitServer, err error) {
	logger.Infow("starting in gateway-only mode", "nodeID", currentNode.NodeID())
	
	s = &LivekitServer{
		config:      conf,
		ioService:   ioService,
		currentNode: currentNode,
		closedChan:  make(chan struct{}),
		// Gateway mode: no RTC, room, signal, or TURN services
		rtcService:   nil,
		whipService:  nil,
		agentService: nil,
		router:       nil,
		roomManager:  nil,
		signalServer: nil,
		turnServer:   nil,
	}

	middlewares := []negroni.Handler{
		negroni.NewRecovery(),
		cors.New(cors.Options{
			AllowOriginFunc: func(origin string) bool {
				return true
			},
			AllowedMethods: []string{"OPTIONS", "HEAD", "GET", "POST", "PATCH", "DELETE"},
			AllowedHeaders: []string{"*"},
			ExposedHeaders: []string{"*"},
			MaxAge:         86400,
		}),
		negroni.HandlerFunc(RemoveDoubleSlashes),
	}
	
	if keyProvider != nil {
		middlewares = append(middlewares, NewAPIKeyAuthMiddleware(keyProvider))
	}

	serverOptions := []any{
		twirp.WithServerHooks(twirp.ChainHooks(
			TwirpLogger(),
			TwirpRequestStatusReporter(),
		)),
	}
	for _, opt := range xtwirp.DefaultServerOptions() {
		serverOptions = append(serverOptions, opt)
	}

	// Only register egress service in gateway mode
	egressServer := livekit.NewEgressServer(egressService, serverOptions...)

	mux := http.NewServeMux()
	if conf.Development {
		mux = http.DefaultServeMux
		mux.HandleFunc("/debug/goroutine", s.debugGoroutines)
	}

	// Only egress endpoints
	xtwirp.RegisterServer(mux, egressServer)
	mux.HandleFunc("/", s.defaultHandler)

	s.httpServer = &http.Server{
		Handler: configureMiddlewares(mux, middlewares...),
	}

	// Prometheus (optional)
	if conf.Prometheus.Port > 0 {
		s.promServer = createPrometheusServer(conf)
	}

	logger.Infow("gateway server initialized", 
		"services", []string{"egress"},
		"port", conf.Port)

	return
}

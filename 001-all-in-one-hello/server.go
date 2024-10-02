package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	uiserver "github.com/temporalio/ui-server/v2/server"
	uiconfig "github.com/temporalio/ui-server/v2/server/config"
	uiserveroptions "github.com/temporalio/ui-server/v2/server/server_options"
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/cluster"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/dynamicconfig"
	temporallog "go.temporal.io/server/common/log"
	"go.temporal.io/server/common/membership/static"
	"go.temporal.io/server/common/metrics"
	sqliteplugin "go.temporal.io/server/common/persistence/sql/sqlplugin/sqlite"
	"go.temporal.io/server/common/primitives"
	sqliteschema "go.temporal.io/server/schema/sqlite"
	"go.temporal.io/server/temporal"
)

func servermain() {
	ip := "127.0.0.1"
	port := 7233
	historyPort := port + 1
	matchingPort := port + 2
	workerPort := port + 3
	uiPort := port + 1000
	metricsPort := uiPort + 1000
	metricsPath := "/metrics"
	pprofPort := 0
	namespace := "default"
	clusterId := uuid.NewString()
	clusterName := "active"
	initialFailoverVersion := int64(1)

	ui := uiserver.NewServer(uiserveroptions.WithConfigProvider(&uiconfig.Config{
		TemporalGRPCAddress: fmt.Sprintf("%s:%d", ip, port),
		Host:                ip,
		Port:                uiPort,
		PublicPath:          "",
		EnableUI:            true,
		UIAssetPath:         "",
		CORS:                uiconfig.CORS{CookieInsecure: true},
		HideLogs:            true,
	}))
	go func() {
		if err := ui.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("UI server error: %s", err)
		}
	}()

	conf := &config.Config{
		Global: config.Global{
			Membership: config.Membership{
				MaxJoinDuration:  30 * time.Second,
				BroadcastAddress: ip,
			},
			Metrics: &metrics.Config{
				Prometheus: &metrics.PrometheusConfig{
					ListenAddress: fmt.Sprintf("%s:%d", ip, metricsPort),
					HandlerPath:   metricsPath,
				},
			},
			PProf: config.PProf{
				Port: pprofPort,
			},
		},
		Persistence: config.Persistence{
			DefaultStore:     "sqlite-default",
			VisibilityStore:  "sqlite-default",
			NumHistoryShards: 1,
			DataStores: map[string]config.DataStore{
				"sqlite-default": {
					SQL: &config.SQL{
						PluginName: sqliteplugin.PluginName,
						ConnectAttributes: map[string]string{
							"mode":  "memory",
							"cache": "shared",
						},
						DatabaseName: "temporal",
					},
				},
			},
		},
		ClusterMetadata: &cluster.Config{
			EnableGlobalNamespace:    false,
			FailoverVersionIncrement: 10,
			MasterClusterName:        clusterName,
			CurrentClusterName:       clusterName,
			ClusterInformation: map[string]cluster.ClusterInformation{
				clusterName: {
					Enabled:                true,
					InitialFailoverVersion: initialFailoverVersion,
					RPCAddress:             fmt.Sprintf("%s:%d", ip, port),
					ClusterID:              clusterId,
				},
			},
		},
		DCRedirectionPolicy: config.DCRedirectionPolicy{
			Policy: "noop",
		},
		Services: map[string]config.Service{
			"frontend": {
				RPC: config.RPC{
					GRPCPort: port,
					BindOnIP: ip,
				},
			},
			"history": {
				RPC: config.RPC{
					GRPCPort: historyPort,
					BindOnIP: ip,
				},
			},
			"matching": {
				RPC: config.RPC{
					GRPCPort: matchingPort,
					BindOnIP: ip,
				},
			},
			"worker": {
				RPC: config.RPC{
					GRPCPort: workerPort,
					BindOnIP: ip,
				},
			},
		},
		Archival: config.Archival{
			History: config.HistoryArchival{
				State: "disabled",
			},
			Visibility: config.VisibilityArchival{
				State: "disabled",
			},
		},
		NamespaceDefaults: config.NamespaceDefaults{
			Archival: config.ArchivalNamespaceDefaults{
				History: config.HistoryArchivalNamespaceDefaults{
					State: "disabled",
				},
				Visibility: config.VisibilityArchivalNamespaceDefaults{
					State: "disabled",
				},
			},
		},
		PublicClient: config.PublicClient{
			HostPort: fmt.Sprintf("%s:%d", ip, port),
		},
	}
	if err := sqliteschema.CreateNamespaces(conf.Persistence.DataStores["sqlite-default"].SQL, sqliteschema.NewNamespaceConfig(clusterName, namespace, false)); err != nil {
		log.Fatalf("unable to create namespace: %s", err)
	}
	authorizer, err := authorization.GetAuthorizerFromConfig(&conf.Global.Authorization)
	if err != nil {
		log.Fatalf("unable to create authorizer: %s", err)
	}
	logger := temporallog.NewNoopLogger().With()
	claimMapper, err := authorization.GetClaimMapperFromConfig(&conf.Global.Authorization, logger)
	if err != nil {
		log.Fatalf("unable to create claim mapper: %s", err)
	}

	dynConf := make(dynamicconfig.StaticClient)
	dynConf[dynamicconfig.HistoryCacheHostLevelMaxSize.Key()] = 8096
	dynConf[dynamicconfig.FrontendMaxNamespaceVisibilityRPSPerInstance.Key()] = 100
	dynConf[dynamicconfig.ForceSearchAttributesCacheRefreshOnRead.Key()] = true
	dynConf[dynamicconfig.FrontendPersistenceMaxQPS.Key()] = 10000
	dynConf[dynamicconfig.HistoryPersistenceMaxQPS.Key()] = 45000

	server, err := temporal.NewServer(
		temporal.WithConfig(conf),
		temporal.ForServices(temporal.DefaultServices),
		temporal.WithStaticHosts(map[primitives.ServiceName]static.Hosts{
			primitives.FrontendService: static.SingleLocalHost(fmt.Sprintf("%s:%d", ip, port)),
			primitives.HistoryService:  static.SingleLocalHost(fmt.Sprintf("%s:%d", ip, historyPort)),
			primitives.MatchingService: static.SingleLocalHost(fmt.Sprintf("%s:%d", ip, matchingPort)),
			primitives.WorkerService:   static.SingleLocalHost(fmt.Sprintf("%s:%d", ip, workerPort)),
		}),
		temporal.WithLogger(logger),
		temporal.WithAuthorizer(authorizer),
		temporal.WithClaimMapper(func(*config.Config) authorization.ClaimMapper { return claimMapper }),
		temporal.WithDynamicConfigClient(dynConf))
	if err != nil {
		log.Fatalf("unable to start server: %s", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("unable to start server: %s", err)
	}
	defer server.Stop()
	log.Printf("%-8s %v:%v", "Server:", ip, port)
	log.Printf("%-8s http://%v:%v", "UI:", ip, uiPort)
	log.Printf("%-8s http://%v:%v/metrics", "Metrics:", ip, metricsPort)

	select {}
}

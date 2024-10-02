package main

import (
	"log"
	"log/slog"

	"github.com/google/uuid"
	"github.com/temporalio/cli/temporalcli/devserver"
)

// skipping logger
func servermain(
	frontendIP string,
	frontendPort int,
	namespace string,
	clusterID string,
) {
	//must register namespace as a search attribute
	//"go.temporal.io/server/temporal"
	//s, err := temporal.NewServer()
}

func devservermain() {
	ip := "127.0.0.1"
	port := 7233
	uiPort := port + 1000
	pprofPort := devserver.MustGetFreePort(ip)
	if err := devserver.CheckPortFree(ip, port); err != nil {
		log.Fatalf("port %v is not free: %v", port, err)
	}
	if err := devserver.CheckPortFree(ip, uiPort); err != nil {
		log.Fatalf("port %v is not free: %v", uiPort, err)
	}

	opts := devserver.StartOptions{
		FrontendIP:             ip,
		FrontendPort:           port,
		Namespaces:             []string{"default"},
		ClusterID:              uuid.NewString(),
		MasterClusterName:      "active",
		CurrentClusterName:     "active",
		InitialFailoverVersion: 1,
		Logger:                 slog.Default(),
		LogLevel:               slog.LevelWarn,
		UIIP:                   ip,
		UIPort:                 uiPort,
		UIAssetPath:            "",
		UICodecEndpoint:        "",
		PublicPath:             "",
		DatabaseFile:           "",
		MetricsPort:            pprofPort,
		PProfPort:              0,
		SqlitePragmas:          map[string]string{},
		FrontendHTTPPort:       0,
		EnableGlobalNamespace:  false,
		DynamicConfigValues: map[string]any{
			"system.forceSearchAttributesCacheRefreshOnRead": true,
			"frontend.persistenceMaxQPS":                     10000,
			"history.persistenceMaxQPS":                      45000,
		},
		LogConfig:        nil,
		GRPCInterceptors: nil,
	}

	s, err := devserver.Start(opts)
	if err != nil {
		log.Fatalf("unable to start server: %v", err)
	}
	defer s.Stop()
	slog.Info("Server started", "ip", ip, "port", port, "uiPort", uiPort, "metricsPort", opts.MetricsPort)
	log.Printf("%-8s %v:%v", "Server:", opts.FrontendIP, opts.FrontendPort)
	log.Printf("%-8s http://%v:%v%v", "UI:", opts.UIIP, opts.UIPort, opts.PublicPath)
	log.Printf("%-8s http://%v:%v/metrics", "Metrics:", opts.FrontendIP, opts.MetricsPort)
	select {}

}

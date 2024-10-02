package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/temporalio/cli/temporalcli/devserver"
	"github.com/temporalio/ui-server/v2/server/version"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/server/common/headers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

type TemporalServerStartDevCommand struct {
	DbFilename         string
	Namespace          []string
	Port               int
	HttpPort           int
	MetricsPort        int
	UiPort             int
	Headless           bool
	Ip                 string
	UiIp               string
	UiPublicPath       string
	UiAssetPath        string
	UiCodecEndpoint    string
	SqlitePragma       []string
	DynamicConfigValue []string
	LogConfig          bool
	SearchAttribute    []string
}

func NewTemporalServerStartDevCommand() *TemporalServerStartDevCommand {
	var s TemporalServerStartDevCommand
	s.DbFilename = ""
	s.Namespace = nil
	s.Port = 7233
	s.HttpPort = 0
	s.MetricsPort = 0
	s.UiPort = 0
	s.Headless = false
	s.Ip = "127.0.0.1"
	s.UiIp = ""
	s.UiPublicPath = ""
	s.UiAssetPath = ""
	s.UiCodecEndpoint = ""
	s.SqlitePragma = nil
	s.DynamicConfigValue = nil
	s.LogConfig = false
	s.SearchAttribute = nil
	return &s
}

func (t *TemporalServerStartDevCommand) prepareSearchAttributes() (map[string]enums.IndexedValueType, error) {
	opts, err := stringKeysValues(t.SearchAttribute)
	if err != nil {
		return nil, fmt.Errorf("invalid search attributes: %w", err)
	}
	attrs := make(map[string]enums.IndexedValueType, len(opts))
	for k, v := range opts {
		// Case-insensitive index type lookup
		var valType enums.IndexedValueType
		for valTypeName, valTypeOrd := range enums.IndexedValueType_shorthandValue {
			if strings.EqualFold(v, valTypeName) {
				valType = enums.IndexedValueType(valTypeOrd)
				break
			}
		}
		if valType == 0 {
			return nil, fmt.Errorf("invalid search attribute value type %q", v)
		}
		attrs[k] = valType
	}
	return attrs, nil
}

func (t *TemporalServerStartDevCommand) registerSearchAttributes(
	cctx context.Context,
	attrs map[string]enums.IndexedValueType,
	namespaces []string,
) error {
	if len(attrs) == 0 {
		return nil
	}

	conn, err := grpc.NewClient(
		fmt.Sprintf("%v:%v", t.Ip, t.Port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed creating client to register search attributes: %w", err)
	}
	defer conn.Close()
	client := operatorservice.NewOperatorServiceClient(conn)
	// Call for each namespace
	for _, ns := range namespaces {
		_, err := client.AddSearchAttributes(cctx, &operatorservice.AddSearchAttributesRequest{
			Namespace:        ns,
			SearchAttributes: attrs,
		})
		if err != nil {
			return fmt.Errorf("failed registering search attributes: %w", err)
		}
	}
	return nil
}

func stringKeysValues(s []string) (map[string]string, error) {
	ret := make(map[string]string, len(s))
	for _, item := range s {
		pieces := strings.SplitN(item, "=", 2)
		if len(pieces) != 2 {
			return nil, fmt.Errorf("missing expected '=' in %q", item)
		}
		ret[pieces[0]] = pieces[1]
	}
	return ret, nil
}

// May be empty result if can't get user home dir
func defaultEnvConfigFile(appName, configName string) string {
	// No env file if no $HOME
	if dir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dir, ".config", appName, configName+".yaml")
	}
	return ""
}

func persistentClusterID() string {
	// If there is not a database file in use, we want a cluster ID to be the same
	// for every re-run, so we set it as an environment config in a special env
	// file. We do not error if we can neither read nor write the file.
	file := defaultEnvConfigFile("temporalio", "version-info")
	if file == "" {
		// No file, can do nothing here
		return uuid.NewString()
	}
	// Try to get existing first
	env, _ := readEnvConfigFile(file)
	if id := env["default"]["cluster-id"]; id != "" {
		return id
	}
	// Create and try to write
	id := uuid.NewString()
	_ = writeEnvConfigFile(file, map[string]map[string]string{"default": {"cluster-id": id}})
	return id
}

func writeEnvConfigFile(file string, env map[string]map[string]string) error {
	b, err := yaml.Marshal(map[string]any{"env": env})
	if err != nil {
		return fmt.Errorf("failed marshaling YAML: %w", err)
	}
	// Make parent directories as needed
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return fmt.Errorf("failed making env file parent dirs: %w", err)
	} else if err := os.WriteFile(file, b, 0600); err != nil {
		return fmt.Errorf("failed writing env file: %w", err)
	}
	return nil
}

func readEnvConfigFile(file string) (env map[string]map[string]string, err error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed reading env file: %w", err)
	}
	var m map[string]map[string]map[string]string
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("failed unmarshalling env YAML: %w", err)
	}
	return m["env"], nil
}

// Version is the value put as the default command version. This is often
// replaced at build time via ldflags.
var Version = "0.0.0-DEV"
var buildInfo string

func VersionString() string {
	// To add build-time information to the version string, use
	// go build -ldflags "-X github.com/temporalio/cli/temporalcli.buildInfo=<MyString>"
	var bi = buildInfo
	if bi != "" {
		bi = fmt.Sprintf(", %s", bi)
	}
	return fmt.Sprintf("%s (Server %s, UI %s%s)", Version, headers.ServerVersion, version.UIVersion, bi)
}

var defaultDynamicConfigValues = map[string]any{
	// Make search attributes immediately visible on creation, so users don't
	// have to wait for eventual consistency to happen when testing against the
	// dev-server.  Since it's a very rare thing to create search attributes,
	// we're comfortable that this is very unlikely to mask bugs in user code.
	"system.forceSearchAttributesCacheRefreshOnRead": true,

	// Since we disable the SA cache, we need to bump max QPS accordingly.
	// These numbers were chosen to maintain the ratio between the two that's
	// established in the defaults.
	"frontend.persistenceMaxQPS": 10000,
	"history.persistenceMaxQPS":  45000,
}

func toFriendlyIp(host string) string {
	if host == "127.0.0.1" || host == "::1" {
		return "localhost"
	}
	return devserver.MaybeEscapeIPv6(host)
}

func startserver(ctx context.Context) error {
	t := NewTemporalServerStartDevCommand()
	// Prepare options
	opts := devserver.StartOptions{
		FrontendIP:             t.Ip,
		FrontendPort:           t.Port,
		Namespaces:             append([]string{"default"}, t.Namespace...),
		Logger:                 slog.Default(),
		DatabaseFile:           t.DbFilename,
		MetricsPort:            t.MetricsPort,
		FrontendHTTPPort:       t.HttpPort,
		ClusterID:              uuid.NewString(),
		MasterClusterName:      "active",
		CurrentClusterName:     "active",
		InitialFailoverVersion: 1,
	}
	/*
		// Set the log level value of the server to the overall log level given to the
		// CLI. But if it is "never" we have to do a special value, and if it was
		// never changed, we have to use the default of "warn" instead of the CLI
		// default of "info" since server is noisier.
		logLevel := t.Parent.Parent.LogLevel.Value
		if !t.Parent.Parent.LogLevel.ChangedFromDefault {
			logLevel = "warn"
		}
		if logLevel == "never" {
			opts.LogLevel = 100
		} else if err := opts.LogLevel.UnmarshalText([]byte(logLevel)); err != nil {
			return fmt.Errorf("invalid log level %q: %w", logLevel, err)
		}
	*/
	if err := devserver.CheckPortFree(opts.FrontendIP, opts.FrontendPort); err != nil {
		return fmt.Errorf("can't set frontend port %d: %w", opts.FrontendPort, err)
	}
	if err := devserver.CheckPortFree(opts.FrontendIP, opts.FrontendHTTPPort); err != nil {
		return fmt.Errorf("can't set frontend HTTP port %d: %w", opts.FrontendHTTPPort, err)
	}
	// Setup UI
	if !t.Headless {
		opts.UIIP, opts.UIPort = t.UiIp, t.UiPort
		if opts.UIIP == "" {
			opts.UIIP = t.Ip
		}
		if opts.UIPort == 0 {
			opts.UIPort = t.Port + 1000
			if opts.UIPort > 65535 {
				opts.UIPort = 65535
			}
			if err := devserver.CheckPortFree(opts.UIIP, opts.UIPort); err != nil {
				return fmt.Errorf("can't use default UI port %d (%d + 1000): %w", opts.UIPort, t.Port, err)
			}
		} else {
			if err := devserver.CheckPortFree(opts.UIIP, opts.UIPort); err != nil {
				return fmt.Errorf("can't set UI port %d: %w", opts.UIPort, err)
			}
		}
		opts.UIAssetPath, opts.UICodecEndpoint, opts.PublicPath = t.UiAssetPath, t.UiCodecEndpoint, t.UiPublicPath
	}
	// Pragmas and dyn config
	var err error
	/*
		if opts.SqlitePragmas, err = stringKeysValues(t.SqlitePragma); err != nil {
			return fmt.Errorf("invalid pragma: %w", err)
		} else if opts.DynamicConfigValues, err = stringKeysJSONValues(t.DynamicConfigValue, true); err != nil {
			return fmt.Errorf("invalid dynamic config values: %w", err)
		}
	*/
	// We have to convert all dynamic config values that JSON number to int if we
	// can because server dynamic config expecting int won't work with the default
	// float JSON unmarshal uses
	for k, v := range opts.DynamicConfigValues {
		if num, ok := v.(json.Number); ok {
			if newV, err := num.Int64(); err == nil {
				// Dynamic config only accepts int type, not int32 nor int64
				opts.DynamicConfigValues[k] = int(newV)
			} else if newV, err := num.Float64(); err == nil {
				opts.DynamicConfigValues[k] = newV
			} else {
				return fmt.Errorf("invalid JSON value for key %q", k)
			}
		}
	}

	// Apply set of default dynamic config values if not already present
	for k, v := range defaultDynamicConfigValues {
		if _, ok := opts.DynamicConfigValues[k]; !ok {
			if opts.DynamicConfigValues == nil {
				opts.DynamicConfigValues = map[string]any{}
			}
			opts.DynamicConfigValues[k] = v
		}
	}

	// Prepare search attributes for adding before starting server
	searchAttrs, err := t.prepareSearchAttributes()
	if err != nil {
		return err
	}

	// If not using DB file, set persistent cluster ID
	if t.DbFilename == "" {
		opts.ClusterID = persistentClusterID()
	}
	/*
		// Log config if requested
		if t.LogConfig {
			opts.LogConfig = func(b []byte) {
				cctx.Logger.Info("Logging config")
				_, _ = cctx.Options.Stderr.Write(b)
			}
		}
	*/
	// Grab a free port for metrics ahead-of-time so we know what port is selected
	if opts.MetricsPort == 0 {
		opts.MetricsPort = devserver.MustGetFreePort(opts.FrontendIP)
	} else {
		if err := devserver.CheckPortFree(opts.FrontendIP, opts.MetricsPort); err != nil {
			return fmt.Errorf("can't set metrics port %d: %w", opts.MetricsPort, err)
		}
	}

	// Start, wait for context complete, then stop
	s, err := devserver.Start(opts)
	if err != nil {
		return fmt.Errorf("failed starting server: %w", err)
	}
	defer s.Stop()

	// Register search attributes
	if err := t.registerSearchAttributes(ctx, searchAttrs, opts.Namespaces); err != nil {
		return err
	}

	log.Printf("CLI %v\n", VersionString())
	//cctx.Printer.Printlnf("CLI %v\n", VersionString())
	log.Printf("%-8s %v:%v", "Server:", toFriendlyIp(opts.FrontendIP), opts.FrontendPort)
	if !t.Headless {
		log.Printf("%-8s http://%v:%v%v", "UI:", toFriendlyIp(opts.UIIP), opts.UIPort, opts.PublicPath)
	}
	log.Printf("%-8s http://%v:%v/metrics", "Metrics:", toFriendlyIp(opts.FrontendIP), opts.MetricsPort)
	<-ctx.Done()
	log.Printf("Stopping server...")
	return nil
}

func main() {
	go startserver(context.Background())
	go workermain()
	go clientmain()
	select {}
}

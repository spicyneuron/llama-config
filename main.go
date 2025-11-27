package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spicyneuron/llama-matchmaker/config"
	"github.com/spicyneuron/llama-matchmaker/logger"
	"github.com/spicyneuron/llama-matchmaker/proxy"
)

// configFiles allows multiple -config flags
type configFiles []string

func (c *configFiles) String() string {
	return fmt.Sprint(*c)
}

func (c *configFiles) Set(value string) error {
	*c = append(*c, value)
	return nil
}

// ProxyServer tracks a running proxy server
type ProxyServer struct {
	server *http.Server
	config config.ProxyConfig
}

type fileWatcher interface {
	Add(name string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

type realWatcher struct {
	*fsnotify.Watcher
}

func (w *realWatcher) Events() <-chan fsnotify.Event {
	return w.Watcher.Events
}

func (w *realWatcher) Errors() <-chan error {
	return w.Watcher.Errors
}

var (
	runningServers []*ProxyServer
	serversMutex   sync.RWMutex
	currentConfig  *config.Config
	watchedFiles   []string
	configWatcher  fileWatcher
	configPaths    configFiles
	overrides      config.CliOverrides
	reloadMutex    sync.Mutex
	reloadTimer    *time.Timer
	watcherMutex   sync.Mutex
	watchFactory   = func() (fileWatcher, error) {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		return &realWatcher{Watcher: w}, nil
	}
	startAllProxiesFn func(cfg *config.Config) error = startAllProxies
	stopAllProxiesFn  func()                         = stopAllProxies
	reloadConfigFn    func()
)

func init() {
	reloadConfigFn = reloadConfig
}

func main() {
	var (
		listenAddr = flag.String("listen", "", "Address to listen on (ex: localhost:8081)")
		targetURL  = flag.String("target", "", "Target URL to proxy to (ex: http://localhost:8080)")
		sslCert    = flag.String("ssl-cert", "", "SSL certificate file (ex: cert.pem)")
		sslKey     = flag.String("ssl-key", "", "SSL key file (ex: key.pem)")
		timeout    = flag.Duration("timeout", 0, "Timeout for requests to target (ex: 60s)")
		debug      = flag.Bool("debug", false, "Print debug logs")
	)

	flag.Var(&configPaths, "config", "Path to YAML configuration (can be specified multiple times)")
	flag.StringVar(listenAddr, "l", "", "Alias for -listen")
	flag.StringVar(targetURL, "t", "", "Alias for -target")
	flag.StringVar(sslCert, "s", "", "Alias for -ssl-cert")
	flag.StringVar(sslKey, "k", "", "Alias for -ssl-key")
	flag.DurationVar(timeout, "T", 0, "Alias for -timeout")
	flag.BoolVar(debug, "d", false, "Alias for -debug")
	flag.Var(&configPaths, "c", "Alias for -config")

	flag.Usage = func() {
		fmt.Println("llama-matchmaker: Match LLM requests to transform settings / responses")
		fmt.Println()
		fmt.Println("Usage: llama-matchmaker -config <config.yml> [-config <routes.yml> ...]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -config, -c string")
		fmt.Println("        Path to YAML configuration (can be specified multiple times)")
		fmt.Println("  -listen, -l string")
		fmt.Println("        Address to listen on (ex: localhost:8081)")
		fmt.Println("  -target, -t string")
		fmt.Println("        Target URL to proxy to (ex: http://localhost:8080)")
		fmt.Println("  -ssl-cert, -s string")
		fmt.Println("        SSL certificate file (ex: cert.pem)")
		fmt.Println("  -ssl-key, -k string")
		fmt.Println("        SSL key file (ex: key.pem)")
		fmt.Println("  -timeout, -T duration")
		fmt.Println("        Timeout for requests to target (ex: 60s)")
		fmt.Println("  -debug, -d")
		fmt.Println("        Print debug logs")
		fmt.Println()
		fmt.Println("For more information and examples, visit:")
		fmt.Println("  https://github.com/spicyneuron/llama-matchmaker")
	}

	flag.Parse()

	if len(configPaths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	overrides = config.CliOverrides{
		Listen:  *listenAddr,
		Target:  *targetURL,
		Timeout: *timeout,
		SSLCert: *sslCert,
		SSLKey:  *sslKey,
		Debug:   *debug,
	}

	cfg, files, err := config.Load(configPaths, overrides)
	if err != nil {
		logger.Fatal("Failed to load config", "err", err)
	}
	currentConfig = cfg
	watchedFiles = files

	if err := startAllProxiesFn(cfg); err != nil {
		logger.Fatal("Failed to start proxies", "err", err)
	}

	if err := setWatcher(files); err != nil {
		logger.Fatal("Failed to setup file watcher", "err", err)
	}
	defer closeWatcher()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	logger.Info("Watching for config changes", "watched_files", len(files))

	<-sigCh
	logger.Info("Shutdown requested", "proxies", len(runningServers))
	stopAllProxies()
	logger.Info("Shutdown complete")
}

func CreateServer(cfg config.ProxyConfig, handler http.Handler) *http.Server {
	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}

	if cfg.SSLCert != "" && cfg.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey)
		if err != nil {
			logger.Fatal("Failed to load SSL certificates", "err", err)
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	if cfg.Timeout > 0 {
		server.IdleTimeout = cfg.Timeout
	}

	return server
}

func startProxy(proxyCfg config.ProxyConfig) (*ProxyServer, error) {
	targetURLParsed, err := url.Parse(proxyCfg.Target)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(targetURLParsed)
	reverseProxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		logger.Error("Reverse proxy error",
			"listen", proxyCfg.Listen,
			"target_host", targetURLParsed.Host,
			"method", req.Method,
			"path", req.URL.Path,
			"err", err)
		http.Error(rw, "Bad Gateway", http.StatusBadGateway)
	}

	// Configure transport with optimized settings for mobile connections
	transport := &http.Transport{
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	if proxyCfg.Timeout > 0 {
		transport.TLSHandshakeTimeout = proxyCfg.Timeout
		transport.ResponseHeaderTimeout = proxyCfg.Timeout
	}

	reverseProxy.Transport = transport

	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		originalDirector(req)
		proxy.ModifyRequest(req, proxyCfg.Routes)
	}

	reverseProxy.ModifyResponse = func(resp *http.Response) error {
		return proxy.ModifyResponse(resp, proxyCfg.Routes)
	}

	server := CreateServer(proxyCfg, reverseProxy)

	ps := &ProxyServer{
		server: server,
		config: proxyCfg,
	}

	// Log start before launching the server goroutine to keep ordering intuitive
	logListen := proxyCfg.Listen
	if proxyCfg.SSLCert != "" && proxyCfg.SSLKey != "" {
		logListen = "https://" + logListen
		logger.Info("Starting HTTPS proxy", "listen", logListen, "target", proxyCfg.Target)
	} else {
		logListen = "http://" + logListen
		logger.Info("Starting HTTP proxy", "listen", logListen, "target", proxyCfg.Target)
	}

	go func() {
		var err error
		if proxyCfg.SSLCert != "" && proxyCfg.SSLKey != "" {
			err = server.ListenAndServeTLS(proxyCfg.SSLCert, proxyCfg.SSLKey)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Proxy server stopped with error", "listen", proxyCfg.Listen, "err", err)
		}
	}()

	return ps, nil
}

func stopProxy(ps *ProxyServer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Debug("Stopping proxy", "listen", ps.config.Listen)
	if err := ps.server.Shutdown(ctx); err != nil {
		logger.Error("Error during proxy shutdown", "listen", ps.config.Listen, "err", err)
	}
}

func stopAllProxies() {
	serversMutex.Lock()
	defer serversMutex.Unlock()

	logger.Info("Stopping proxies", "count", len(runningServers))
	var wg sync.WaitGroup
	for _, ps := range runningServers {
		wg.Add(1)
		go func(p *ProxyServer) {
			defer wg.Done()
			stopProxy(p)
		}(ps)
	}
	wg.Wait()
	runningServers = nil

	// Give OS time to fully release the ports
	time.Sleep(100 * time.Millisecond)
}

func startAllProxies(cfg *config.Config) error {
	serversMutex.Lock()
	defer serversMutex.Unlock()

	debugEnabled := false
	for _, proxyCfg := range cfg.Proxies {
		if proxyCfg.Debug {
			debugEnabled = true
			break
		}
	}
	logger.EnableDebug(debugEnabled)

	logResolvedConfig(cfg)

	for i, proxyCfg := range cfg.Proxies {
		ps, err := startProxy(proxyCfg)
		if err != nil {
			logger.Fatal("Failed to start proxy", "index", i, "err", err)
			return err
		}
		runningServers = append(runningServers, ps)
	}

	logger.Debug("All proxies started", "count", len(runningServers))
	return nil
}

func logResolvedConfig(cfg *config.Config) {
	if !logger.IsDebug() {
		return
	}

	totalProxyRoutes := 0
	sslEnabled := 0

	for i, p := range cfg.Proxies {
		logListen := p.Listen
		if p.SSLCert != "" && p.SSLKey != "" {
			logListen = "https://" + logListen
		} else {
			logListen = "http://" + logListen
		}

		reqOps := 0
		respOps := 0
		for _, r := range p.Routes {
			reqOps += len(r.OnRequest)
			respOps += len(r.OnResponse)
		}

		logger.Debug(fmt.Sprintf("Proxy %d configured", i+1),
			"listen", logListen,
			"target", p.Target,
			"timeout", p.Timeout,
			"routes", len(p.Routes),
			"request_actions", reqOps,
			"response_actions", respOps,
		)
		totalProxyRoutes += len(p.Routes)
		if p.SSLCert != "" && p.SSLKey != "" {
			sslEnabled++
		}
	}

}

func setupFileWatcher(watchedFiles []string) (fileWatcher, error) {
	watcher, err := watchFactory()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	for _, file := range watchedFiles {
		if err := watcher.Add(file); err != nil {
			logger.Error("Failed to watch file", "file", file, "err", err)
			continue
		}
		logger.Debug("Watching file", "file", file)
	}

	return watcher, nil
}

func setWatcher(files []string) error {
	watcherMutex.Lock()
	defer watcherMutex.Unlock()

	if configWatcher != nil {
		configWatcher.Close()
	}

	watcher, err := setupFileWatcher(files)
	if err != nil {
		return err
	}
	configWatcher = watcher
	go watchForChanges(watcher)
	return nil
}

func closeWatcher() {
	watcherMutex.Lock()
	defer watcherMutex.Unlock()

	if configWatcher != nil {
		configWatcher.Close()
	}
}

func watchForChanges(watcher fileWatcher) {
	for {
		select {
		case event, ok := <-watcher.Events():
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				logger.Debug("Config file changed", "file", event.Name, "op", event.Op.String())
				debounceReload()
			}
		case err, ok := <-watcher.Errors():
			if !ok {
				return
			}
			logger.Error("File watcher error", "err", err)
		}
	}
}

func debounceReload() {
	reloadMutex.Lock()
	defer reloadMutex.Unlock()

	if reloadTimer != nil {
		reloadTimer.Stop()
	}

	reloadTimer = time.AfterFunc(200*time.Millisecond, func() {
		logger.Info("Config file changed, reloading...")
		if reloadConfigFn != nil {
			reloadConfigFn()
		}
	})
}

func reloadConfig() {
	newCfg, newFiles, err := config.Load(configPaths, overrides)
	if err != nil {
		logger.Error("Failed to reload config, keeping current config", "err", err)
		return
	}

	logger.Info("Successfully loaded new config")

	stopAllProxiesFn()

	if err := startAllProxiesFn(newCfg); err != nil {
		logger.Error("Failed to start proxies with new config, attempting to restore previous config", "err", err)
		if err := startAllProxiesFn(currentConfig); err != nil {
			logger.Fatal("Failed to restore previous config", "err", err)
		}
		logger.Info("Restored previous config")
		return
	}

	currentConfig = newCfg
	watchedFiles = newFiles

	if err := setWatcher(newFiles); err != nil {
		logger.Error("Failed to update file watcher after reload", "err", err)
	}

	logger.Info("Config reloaded successfully", "proxies", len(newCfg.Proxies), "watched_files", len(newFiles))
}

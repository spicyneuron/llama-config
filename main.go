package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/spicyneuron/llama-config-proxy/config"
	"github.com/spicyneuron/llama-config-proxy/logger"
	"github.com/spicyneuron/llama-config-proxy/proxy"
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

func main() {
	var (
		configPaths configFiles
		listenAddr  = flag.String("listen", "", "Address to listen on (ex: localhost:8081)")
		targetURL   = flag.String("target", "", "Target URL to proxy to (ex: http://localhost:8080)")
		sslCert     = flag.String("ssl-cert", "", "SSL certificate file (ex: cert.pem)")
		sslKey      = flag.String("ssl-key", "", "SSL key file (ex: key.pem)")
		timeout     = flag.Duration("timeout", 0, "Timeout for requests to target (ex: 60s)")
		debug       = flag.Bool("debug", false, "Print debug logs")
	)

	flag.Var(&configPaths, "config", "Path to YAML configuration (can be specified multiple times)")

	flag.Usage = func() {
		fmt.Println("llama-config-proxy: Automatically apply optimal settings to LLM requests")
		fmt.Println()
		fmt.Println("Usage: llama-config-proxy -config <config.yml> [-config <rules.yml> ...]")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("For more information and examples, visit:")
		fmt.Println("  https://github.com/spicyneuron/llama-config-proxy")
	}

	flag.Parse()

	if len(configPaths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	overrides := config.CliOverrides{
		Listen:  *listenAddr,
		Target:  *targetURL,
		Timeout: *timeout,
		SSLCert: *sslCert,
		SSLKey:  *sslKey,
		Debug:   *debug,
	}

	cfg, err := config.Load(configPaths, overrides)
	if err != nil {
		logger.Fatal("Failed to load config", "err", err)
	}

	debugEnabled := false
	for _, proxyCfg := range cfg.Proxies {
		if proxyCfg.Debug {
			debugEnabled = true
			break
		}
	}

	logger.EnableDebug(debugEnabled)
	if len(configPaths) == 1 {
		logger.Info("Loaded config file", "path", configPaths[0])
	} else {
		logger.Info("Loaded config files", "count", len(configPaths), "paths", configPaths)
	}

	errCh := make(chan error, len(cfg.Proxies))

	for i, proxyCfg := range cfg.Proxies {
		proxyCfg := proxyCfg // capture loop variable
		proxyConfigForHandlers := &config.Config{
			Proxies: []config.ProxyConfig{proxyCfg},
			Rules:   proxyCfg.Rules,
		}

		targetURLParsed, err := url.Parse(proxyCfg.Target)
		if err != nil {
			logger.Fatal("Proxy has invalid target server URL", "index", i, "err", err)
		}

		reverseProxy := httputil.NewSingleHostReverseProxy(targetURLParsed)

		if proxyCfg.Timeout > 0 {
			reverseProxy.Transport = &http.Transport{
				TLSHandshakeTimeout:   proxyCfg.Timeout,
				ResponseHeaderTimeout: proxyCfg.Timeout,
			}
			logger.Info("Configured proxy timeouts", "listen", proxyCfg.Listen, "timeout", proxyCfg.Timeout)
			logger.Debug("Transport timeouts", "listen", proxyCfg.Listen, "tls_handshake", proxyCfg.Timeout, "response_header", proxyCfg.Timeout)
		}

		originalDirector := reverseProxy.Director
		reverseProxy.Director = func(req *http.Request) {
			logger.Info("Proxy request", "listen", proxyCfg.Listen, "method", req.Method, "path", req.URL.Path)
			originalDirector(req)
			proxy.ModifyRequest(req, proxyConfigForHandlers)
		}

		// Add response modifier
		reverseProxy.ModifyResponse = func(resp *http.Response) error {
			return proxy.ModifyResponse(resp, proxyConfigForHandlers)
		}

		server := createServer(proxyCfg, reverseProxy)

		go func(p config.ProxyConfig, srv *http.Server) {
			if p.SSLCert != "" && p.SSLKey != "" {
				logger.Info("Starting HTTPS proxy", "listen", p.Listen, "target", p.Target)
				errCh <- srv.ListenAndServeTLS(p.SSLCert, p.SSLKey)
			} else {
				logger.Info("Starting HTTP proxy", "listen", p.Listen, "target", p.Target)
				errCh <- srv.ListenAndServe()
			}
		}(proxyCfg, server)
	}

	logger.Fatal("Proxy server failed", "err", <-errCh)
}

func createServer(cfg config.ProxyConfig, handler http.Handler) *http.Server {
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

	server.ReadTimeout = cfg.Timeout
	server.WriteTimeout = cfg.Timeout

	return server
}

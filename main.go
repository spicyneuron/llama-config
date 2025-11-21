package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/spicyneuron/llama-config-proxy/config"
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
		log.Fatalf("Failed to load config: %v", err)
	}

	debugEnabled := false
	for _, proxyCfg := range cfg.Proxies {
		if proxyCfg.Debug {
			debugEnabled = true
			break
		}
	}

	config.SetDebugMode(debugEnabled)
	proxy.SetDebugMode(debugEnabled)
	if len(configPaths) == 1 {
		log.Printf("Loaded config from: %s", configPaths[0])
	} else {
		log.Printf("Loaded config from %d files: %v", len(configPaths), configPaths)
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
			log.Fatalf("Proxy %d has invalid target server URL: %v", i, err)
		}

		reverseProxy := httputil.NewSingleHostReverseProxy(targetURLParsed)

		if proxyCfg.Timeout > 0 {
			reverseProxy.Transport = &http.Transport{
				TLSHandshakeTimeout:   proxyCfg.Timeout,
				ResponseHeaderTimeout: proxyCfg.Timeout,
			}
			log.Printf("Configured timeout for %s: %v", proxyCfg.Listen, proxyCfg.Timeout)
			if proxyCfg.Debug {
				log.Printf("[DEBUG] Transport timeouts: TLS handshake=%v, response header=%v",
					proxyCfg.Timeout, proxyCfg.Timeout)
			}
		}

		originalDirector := reverseProxy.Director
		reverseProxy.Director = func(req *http.Request) {
			log.Printf("[%s] %s %s", proxyCfg.Listen, req.Method, req.URL.Path)
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
				log.Printf("Proxying https://%s to %s", p.Listen, p.Target)
				errCh <- srv.ListenAndServeTLS(p.SSLCert, p.SSLKey)
			} else {
				log.Printf("Proxying http://%s to %s", p.Listen, p.Target)
				errCh <- srv.ListenAndServe()
			}
		}(proxyCfg, server)
	}

	log.Fatalf("Proxy server failed: %v", <-errCh)
}

func createServer(cfg config.ProxyConfig, handler http.Handler) *http.Server {
	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}

	if cfg.SSLCert != "" && cfg.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey)
		if err != nil {
			log.Fatalf("Failed to load SSL certificates: %v", err)
		}
		if cfg.Debug {
			log.Printf("[DEBUG] Loaded SSL certificate from: cert=%s, key=%s",
				cfg.SSLCert, cfg.SSLKey)
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	server.ReadTimeout = cfg.Timeout
	server.WriteTimeout = cfg.Timeout

	return server
}

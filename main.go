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

func main() {
	var (
		configFile = flag.String("config", "", "Path to YAML configuration (required)")
		listenAddr = flag.String("listen", "", "Address to listen on (ex: localhost:8081)")
		targetURL  = flag.String("target", "", "Target URL to proxy to (ex: http://localhost:8080)")
		sslCert    = flag.String("ssl-cert", "", "SSL certificate file (ex: cert.pem)")
		sslKey     = flag.String("ssl-key", "", "SSL key file (ex: key.pem)")
		timeout    = flag.Duration("timeout", 0, "Timeout for requests to target (ex: 60s)")
		debug      = flag.Bool("debug", false, "Print debug logs")
	)

	flag.Usage = func() {
		fmt.Println("llama-config-proxy: Automatically apply optimal settings to LLM requests")
		fmt.Println()
		fmt.Println("Usage: llama-config-proxy --config <config.yml>")
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("For more information and examples, visit:")
		fmt.Println("  https://github.com/spicyneuron/llama-config-proxy")
	}

	flag.Parse()

	if *configFile == "" {
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

	cfg, err := config.Load(*configFile, overrides)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	proxy.SetDebugMode(cfg.Proxy.Debug)
	log.Printf("Loaded config from: %s", *configFile)

	targetURLParsed, err := url.Parse(cfg.Proxy.Target)
	if err != nil {
		log.Fatalf("Invalid target server URL: %v", err)
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(targetURLParsed)

	if cfg.Proxy.Timeout > 0 {
		reverseProxy.Transport = &http.Transport{
			TLSHandshakeTimeout:   cfg.Proxy.Timeout,
			ResponseHeaderTimeout: cfg.Proxy.Timeout,
		}
		log.Printf("Configured timeout: %v", cfg.Proxy.Timeout)
	}

	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		log.Printf("%s %s", req.Method, req.URL.Path)
		originalDirector(req)
		proxy.ModifyRequest(req, cfg)
	}

	// Add response modifier
	reverseProxy.ModifyResponse = func(resp *http.Response) error {
		return proxy.ModifyResponse(resp, cfg)
	}

	listenAddrFinal := cfg.Proxy.Listen
	server := createServer(listenAddrFinal, reverseProxy, cfg)

	if cfg.Proxy.SSLCert != "" && cfg.Proxy.SSLKey != "" {
		log.Printf("Proxying https://%s to %s", listenAddrFinal, cfg.Proxy.Target)
		log.Fatalf("HTTPS server failed: %v", server.ListenAndServeTLS(cfg.Proxy.SSLCert, cfg.Proxy.SSLKey))
	} else {
		log.Printf("Proxying http://%s to %s", listenAddrFinal, cfg.Proxy.Target)
		log.Fatalf("HTTP server failed: %v", server.ListenAndServe())
	}
}

func createServer(addr string, handler http.Handler, cfg *config.Config) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	if cfg.Proxy.SSLCert != "" && cfg.Proxy.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Proxy.SSLCert, cfg.Proxy.SSLKey)
		if err != nil {
			log.Fatalf("Failed to load SSL certificates: %v", err)
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	server.ReadTimeout = cfg.Proxy.Timeout
	server.WriteTimeout = cfg.Proxy.Timeout

	return server
}

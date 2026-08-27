package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	listenAddress := flag.String("listen", ":8443", "HTTPS listen address")
	targetAddress := flag.String("target", "http://127.0.0.1:8080", "upstream OpenDepot URL")
	certificatePath := flag.String("cert", "", "TLS certificate path")
	privateKeyPath := flag.String("key", "", "TLS private key path")
	flag.Parse()

	if *certificatePath == "" || *privateKeyPath == "" {
		log.Fatal("both --cert and --key are required")
	}

	target, err := url.Parse(*targetAddress)
	if err != nil {
		log.Fatalf("parse target URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	server := &http.Server{
		Addr:      *listenAddress,
		Handler:   proxy,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	fmt.Printf("Provider mirror TLS proxy listening on https://opendepot.localtest.me%s\n", *listenAddress)
	fmt.Printf("Forwarding requests to %s\n", target)

	if err := server.ListenAndServeTLS(*certificatePath, *privateKeyPath); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

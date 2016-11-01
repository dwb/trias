package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

const defaultShell = "/bin/sh"

// TODO: these should be found from sysctl, not hard-coded
const firstEphemeralPort = 49152
const lastEphemeralPort = 65536
const defaultTimeout = 20 * time.Second

var errSSHNotConnected = fmt.Errorf("SSH not connected")

func main() {
	var err error
	defer func() {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err.Error())
			os.Exit(1)
		}
	}()

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = defaultShell
	}

	profile, err := getProfile()
	if err != nil {
		return
	}

	sshClient, err := newKeptaliveSSHClient(profile)
	if err != nil {
		return
	}
	defer sshClient.Close()

	ended := make(chan error)
	defer func() {
		_ = <-ended
	}()

	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	proxyAddr, err := serveHTTPProxy(ctx, sshClient, ended)
	if err != nil {
		return
	}
	proxyURL := "http://" + proxyAddr

	fmt.Printf("\n%s: starting proxied shell\n\n", os.Args[0])

	env := os.Environ()
	shellEnv := make([]string, len(env))
	copy(shellEnv, env)
	shellEnv = append(shellEnv,
		"TRIAS_PROFILE="+profile.Name,
		// some clients prefer the lower-case form of these vars:
		"http_proxy="+proxyURL,
		"HTTP_PROXY="+proxyURL,
		"https_proxy="+proxyURL,
		"HTTPS_PROXY="+proxyURL)

	shell := exec.Command(shellPath)
	shell.Env = shellEnv
	shell.Stdin = os.Stdin
	shell.Stdout = os.Stdout
	shell.Stderr = os.Stderr
	err = shell.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// we don't care if the shell process exits != 0
			err = nil
		} else {
			return
		}
	}

	fmt.Printf("\n%s: leaving proxied shell\n\n", os.Args[0])
}

func getProfile() (p profile, err error) {
	conf, err := loadConfig()
	if err != nil {
		return p, err
	}

	if len(os.Args) < 2 {
		return p, fmt.Errorf("usage: %s PROFILE", os.Args[0])
	}

	profileName := os.Args[1]
	p, ok := conf.Profiles[profileName]
	if !ok {
		return p, fmt.Errorf("'%s' profile not found in config", profileName)
	}

	p.Name = profileName
	return p, nil
}

func serveHTTPProxy(ctx context.Context, sshClient *keptaliveSSHClient, endCh chan<- error) (addr string, err error) {

	var listener net.Listener

	for p := firstEphemeralPort; p <= lastEphemeralPort; p++ {
		addr = fmt.Sprintf("localhost:%d", p)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
	}

	if listener == nil {
		return "", fmt.Errorf(
			"couldn't find proxy port to listen to, checked %d to %d, last error: %s",
			firstEphemeralPort, lastEphemeralPort, err)
	}

	go func() {
		_ = <-ctx.Done()
		listener.Close()
		endCh <- nil
	}()

	httpTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, err error) {
			return sshClient.Dial(network, addr)
		},
	}

	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   defaultTimeout,
	}

	httpServer := &http.Server{
		Addr:         addr,
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				return
			}

			defer r.Body.Close()

			if r.Method == http.MethodConnect {
				handleProxyConnect(w, r, sshClient)
			} else {
				r.RequestURI = ""
				rsp, _ := httpClient.Do(r)
				if rsp == nil {
					return
				}

				defer rsp.Body.Close()

				for h, vs := range rsp.Header {
					for _, v := range vs {
						w.Header().Add(h, v)
					}
				}
				w.WriteHeader(rsp.StatusCode)

				io.Copy(w, rsp.Body)
			}

		}),
	}

	go func() {
		err := httpServer.Serve(listener)
		endCh <- err
	}()

	return
}

func handleProxyConnect(w http.ResponseWriter, r *http.Request, sshClient *keptaliveSSHClient) {
	r.Body.Close()

	wFlusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "can't flush ResponseWriter",
			http.StatusInternalServerError)
		return
	}

	wHijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "can't hijack ResponseWriter",
			http.StatusInternalServerError)
		return
	}

	remote, err := sshClient.Dial("tcp", r.RequestURI)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer remote.Close()

	w.WriteHeader(http.StatusOK)
	wFlusher.Flush()

	httpConn, httpBuf, err := wHijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer httpConn.Close()

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		io.Copy(httpBuf, remote)
		wg.Done()
	}()

	go func() {
		io.Copy(remote, httpBuf)
		wg.Done()
	}()

	wg.Wait()
}

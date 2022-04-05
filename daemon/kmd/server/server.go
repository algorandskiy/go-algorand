// Copyright (C) 2019-2022 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package server

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/algorand/go-deadlock"

	"github.com/algorand/go-algorand/daemon/kmd/api"
	"github.com/algorand/go-algorand/daemon/kmd/session"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/util/tokens"
)

const (
	// NetFilename is the name of the net file in the kmd data dir
	NetFilename = "kmd.net"
	// PIDFilename is the name of the PID file in the kmd data dir
	PIDFilename = "kmd.pid"
	// LockFilename is the name of the lock file in the kmd data dir
	LockFilename = "kmd.lock"
	// DefaultKMDPort is the port that kmd will first try to start on if none is specified
	DefaultKMDPort = 7833
	// DefaultKMDHost is the host that kmd will first try to start on if none is specified
	DefaultKMDHost = "127.0.0.1"
)

// WalletServerConfig is the configuration passed to MakeWalletServer
type WalletServerConfig struct {
	APIToken       string
	DataDir        string
	Address        string
	AllowedOrigins []string
	SessionManager *session.Manager
	Log            logging.Logger
	Timeout        *time.Duration
}

// WalletServer deals with serving API requests
type WalletServer struct {
	WalletServerConfig
	netPath      string
	pidPath      string
	lockPath     string
	sockPath     string
	tmpSocketDir string

	// This mutex protects shutdown, which lets us know if we died unexpectedly
	// or as a result of being killed
	mux      *deadlock.Mutex
	shutdown bool
}

// ValidateConfig returns an error if and only if the passed WalletServerConfig
// is invalid.
func ValidateConfig(cfg WalletServerConfig) error {
	err := tokens.ValidateAPIToken(cfg.APIToken)
	if err != nil {
		return err
	}

	if cfg.DataDir == "" {
		return errDataDirRequired
	}

	if !cfg.SessionManager.Initialized {
		return errSessionManagerRequired
	}

	if cfg.Log == nil {
		return errLogRequired
	}

	return nil
}

// MakeWalletServer takes a WalletServerConfig, and returns a validated,
// configured WalletServer.
func MakeWalletServer(config WalletServerConfig) (*WalletServer, error) {
	err := ValidateConfig(config)
	if err != nil {
		return nil, err
	}

	ws := &WalletServer{
		WalletServerConfig: config,
		netPath:            filepath.Join(config.DataDir, NetFilename),
		pidPath:            filepath.Join(config.DataDir, PIDFilename),
		lockPath:           filepath.Join(config.DataDir, LockFilename),
		mux:                &deadlock.Mutex{},
	}

	return ws, nil
}

// Acquire an exclusive file lock on kmd.lock
func (ws *WalletServer) acquireFileLock() error {
	// Attempt to acquire exclusive lock
	return nil
}

// Release our exclusive lock on kmd.lock
func (ws *WalletServer) releaseFileLock() error {
	return nil
}

// Write out a file containing the address kmd is listening on
func (ws *WalletServer) writeStateFiles(netAddr string) (err error) {
	// netPath file contains path to sock file
	err = ioutil.WriteFile(ws.netPath, []byte(netAddr), 0640)
	if err != nil {
		return
	}
	// pidPath file contains current process ID
	err = ioutil.WriteFile(ws.pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0640)
	return
}

// Delete the state files generated by writeStateFiles
func (ws *WalletServer) deleteStateFiles() {
	os.Remove(ws.pidPath)
	os.Remove(ws.netPath)
}

// makeWatchdogCallback generates a callback function that either 1. does
// nothing if ws.Timeout is nil, or 2. kicks a watchdog timer that will kill
// kmd when it expires.
func (ws *WalletServer) makeWatchdogCallback(kill chan os.Signal) func() {
	// If Timeout is nil, then we will not kill kmd after a timeout
	if ws.Timeout == nil {
		return func() {}
	}

	// After Timeout, send a signal on the kill channel
	timeout := *ws.Timeout
	timer := time.AfterFunc(timeout, func() {
		ws.Log.Infof("killing kmd after timeout")
		kill <- syscall.SIGINT
	})

	// Return a callback function that resets the timer if it hasn't
	// already expired
	return func() {
		// From docs: For a timer created with AfterFunc(d, f), if
		// t.Stop returns false, then the timer has already expired
		// and the function f has been started in its own goroutine
		if timer.Stop() {
			timer.Reset(timeout)
		}
	}
}

// Start begins serving kmd API requests from the configured WalletServer. It
// returns an error if it was unable to start the server. It reads from the
// `kill` channel in order to shut down the server gracefully, and returns a
// `died` channel that will be written after the server exits.
func (ws *WalletServer) Start(kill chan os.Signal) (died chan error, sock string, err error) {
	// Ensure we're the only instance of kmd running in this data directory
	err = ws.acquireFileLock()
	if err != nil {
		return
	}

	// Start the server
	died, sock, err = ws.start(kill)
	if err != nil {
		// Release our file lock if we failed to start
		ws.releaseFileLock()
	}
	return
}

// start does the heavy lifting for Start
func (ws *WalletServer) start(kill chan os.Signal) (died chan error, sock string, err error) {
	// Initialize HTTP server
	watchdogCB := ws.makeWatchdogCallback(kill)
	srv := http.Server{
		Handler: api.Handler(ws.SessionManager, ws.Log, ws.AllowedOrigins, ws.APIToken, watchdogCB),
	}

	// Read the kill channel and shut down the server gracefully
	go func() {
		<-kill

		// Indicate that we're calling Shutdown, so we know we died on purpose
		ws.mux.Lock()
		ws.shutdown = true
		ws.mux.Unlock()

		// Shut down the server
		err := srv.Shutdown(context.Background())
		if err != nil {
			ws.Log.Warnf("non-nil error stopping kmd wallet HTTP server: %s", err)
		}
	}()

	// If the user specified an address, try to use that
	address := ws.Address
	userSpecifiedAddress := true
	if address == "" {
		// Otherwise, use the default host and port
		address = fmt.Sprintf("%s:%d", DefaultKMDHost, DefaultKMDPort)
		userSpecifiedAddress = false
	}

	// Try to listen at host:port
	listener, err := net.Listen("tcp", address)
	if err != nil {
		// User specified an address and we couldn't listen there; return an error
		if userSpecifiedAddress {
			return
		}

		// Try one more time on any open port
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", DefaultKMDHost, 0))
		if err != nil {
			return
		}
	}

	// Write out our net file
	addr := listener.Addr().String()
	err = ws.writeStateFiles(addr)
	if err != nil {
		return
	}

	// We'll send something on this channel when we die
	died = make(chan error)

	// Begin serving requests
	go func() {
		err := srv.Serve(listener)
		ws.mux.Lock()
		defer ws.mux.Unlock()

		// Log to indicate the reason we died
		if !ws.shutdown {
			ws.Log.Errorf("kmd wallet HTTP server died unexpectedly: %s", err)
		} else {
			ws.Log.Infof("kmd wallet HTTP server stopped with: %s", err)
		}

		// Clean up files that we only use while running
		ws.deleteStateFiles()

		// Clean up the session manager gracefully
		ws.SessionManager.Kill()

		// Release our file lock
		ws.releaseFileLock()

		// Tell the main thread we died
		died <- err
	}()

	return died, addr, nil
}

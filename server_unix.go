// +build !windows

package cprpc

import (
	"context"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

var allProcFiles = []*os.File{os.Stdin, os.Stdout, os.Stderr}

// startProcess starts a new process passing it the active listeners. It
// doesn't fork, but starts a new process using the same environment and
// arguments as when it was originally started. This allows for a newly
// deployed binary to be started. It returns the pid of the newly started
// process when successful.
func startProcess() (int, error) {
	execName, err := os.Executable()
	if err != nil {
		return 0, err
	}
	execDir := filepath.Dir(execName)

	for _, f := range allProcFiles {
		defer f.Close()
	}

	// Use the original binary location. This works with symlinks such that if
	// the file it points to has been changed we will use the updated symlink.
	argv0, err := exec.LookPath(os.Args[0])
	if err != nil {
		return 0, err
	}

	process, err := os.StartProcess(argv0, os.Args, &os.ProcAttr{
		Dir:   execDir,
		Env:   os.Environ(),
		Files: allProcFiles,
		Sys:   &syscall.SysProcAttr{},
	})
	if err != nil {
		return 0, err
	}
	return process.Pid, nil
}

func forkChild() (*os.Process, error) {
	execName, err := os.Executable()
	if err != nil {
		return nil, err
	}
	execDir := filepath.Dir(execName)
	// Spawn child process.
	p, err := os.StartProcess(execName, os.Args, &os.ProcAttr{
		Dir: execDir,
		Sys: &syscall.SysProcAttr{},
	})
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Serve accepts connections on the listener and serves requests
// for each incoming connection. Accept blocks until the listener
// returns a non-nil error. The caller typically invokes Accept in a
// go statement.
func (server *Server) Serve(lis net.Listener) error {
	go server.serve(lis)

	sigCh := make(chan os.Signal, 10)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR2)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			return server.Shutdown(context.Background())
		case syscall.SIGHUP, syscall.SIGUSR2:
			p, err := forkChild()
			if err != nil {
				log.Printf("unable to fork child: %v.\n", err)
				continue
			}
			log.Printf("forked child %v.\n", p.Pid)
			return server.Shutdown(context.Background())
		}
	}
}

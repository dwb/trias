package main

import (
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"net"
	"os"
	"time"
)

type keptaliveSSHClient struct {
	user         string
	host         string
	c            *ssh.Client
	failed       chan struct{}
	shuttingDown bool
}

func newKeptaliveSSHClient(p profile) (c *keptaliveSSHClient, err error) {
	user := p.User
	if user == "" {
		user = os.Getenv("USER")
	}

	c = &keptaliveSSHClient{
		user:   user,
		host:   p.Host,
		failed: make(chan struct{}),
	}

	err = c.Connect()
	if err != nil {
		return nil, err
	}

	go c.reconnectLoop()

	return
}

func (c *keptaliveSSHClient) Connect() error {
	agentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return err
	}
	authMethod := ssh.PublicKeysCallback(
		sshagent.NewClient(agentConn).Signers)

	config := &ssh.ClientConfig{
		User: c.user,
		Auth: []ssh.AuthMethod{
			authMethod,
		},
	}

	sshc, err := ssh.Dial("tcp", c.host+":22", config)
	if err != nil {
		return err
	}

	if c.shuttingDown {
		sshc.Close()
		return nil
	}

	c.c = sshc
	return nil
}

func (c *keptaliveSSHClient) Dial(net, addr string) (conn net.Conn, err error) {
	if c.c == nil {
		c.notifyFailed()
		return nil, errSSHNotConnected
	}
	conn, err = c.c.Dial(net, addr)
	if err != nil {
		c.c = nil
		c.notifyFailed()
	}
	return
}

func (c *keptaliveSSHClient) Close() error {
	c.shuttingDown = true
	if c.c == nil {
		return nil
	}
	return c.c.Close()
}

func (c *keptaliveSSHClient) reconnectLoop() {
	for _ = range c.failed {
		for {
			err := c.Connect()
			if err == nil {
				break
			}
			time.Sleep(time.Second)
		}
	}
}

func (c *keptaliveSSHClient) notifyFailed() {
	select {
	case c.failed <- struct{}{}:
	default:
	}
}

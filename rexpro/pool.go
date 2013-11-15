// Copyright 2013 Philip Southam
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package rexpro

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

var nowFunc = time.Now

var ErrPoolExhausted = errors.New("gorexpro: connection pool exhausted")

var errPoolClosed = errors.New("gorexpro: connection pool closed")

type Pool struct {
	Dial         func() (Conn, error)
	TestOnBorrow func(c Conn, t time.Time) error
	MaxIdle      int
	MaxActive    int
	IdleTimeout  time.Duration
	mu           sync.Mutex
	closed       bool
	active       int
	idle         list.List
}

type idleConn struct {
	c Conn
	t time.Time
}

func NewPool(newFn func() (Conn, error), maxIdle int) *Pool {
	return &Pool{Dial: newFn, MaxIdle: maxIdle}
}

func (p *Pool) Get() Conn {
	return &pooledConnection{p: p}
}

func (p *Pool) ActiveCount() int {
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	return active
}

func (p *Pool) Close() error {
	p.mu.Lock()
	idle := p.idle
	p.idle.Init()
	p.closed = true
	p.active -= idle.Len()
	p.mu.Unlock()
	for e := idle.Front(); e != nil; e = e.Next() {
		e.Value.(idleConn).c.Close()
	}
	return nil
}

func (p *Pool) get() (Conn, error) {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("rexpro: get on closed pool")
	}

	if timeout := p.IdleTimeout; timeout > 0 {
		for i, n := 0, p.idle.Len(); i < n; i++ {
			e := p.idle.Back()
			if e == nil {
				break
			}
			ic := e.Value.(idleConn)
			if ic.t.Add(timeout).After(nowFunc()) {
				break
			}
			p.idle.Remove(e)
			p.active -= 1
			p.mu.Unlock()
			ic.c.Close()
			p.mu.Lock()
		}
	}

	for i, n := 0, p.idle.Len(); i < n; i++ {
		e := p.idle.Front()
		if e == nil {
			break
		}
		ic := e.Value.(idleConn)
		p.idle.Remove(e)
		test := p.TestOnBorrow
		p.mu.Unlock()
		if test == nil || test(ic.c, ic.t) == nil {
			return ic.c, nil
		}
		ic.c.Close()
		p.mu.Lock()
		p.active -= 1
	}

	if p.MaxActive > 0 && p.active >= p.MaxActive {
		p.mu.Unlock()
		return nil, ErrPoolExhausted
	}

	dial := p.Dial
	p.active += 1
	p.mu.Unlock()
	c, err := dial()
	if err != nil {
		p.mu.Lock()
		p.active -= 1
		p.mu.Unlock()
		c = nil
	}
	return c, err
}

func (p *Pool) put(c Conn) error {
	if c != nil {
		p.mu.Lock()
		p.active -= 1
		p.mu.Unlock()
		return c.Close()
	}
	return nil
}

type pooledConnection struct {
	c   Conn
	err error
	p   *Pool
}

func (c *pooledConnection) get() error {
	if c.err == nil && c.c == nil {
		c.c, c.err = c.p.get()
	}
	return c.err
}

func (c *pooledConnection) Close() (err error) {
	if c.c != nil {
		//c.c.Flush()
		c.p.put(c.c)
		c.c = nil
		c.err = errPoolClosed
	}
	return err
}

func (c *pooledConnection) DoScript(script string, bindings map[string]interface{}) ([]interface{}, error) {
	if err := c.get(); err != nil {
		return nil, err
	}
	return c.c.DoScript(script, bindings)
}

func (c *pooledConnection) NewSession() (Session, error) {
	if err := c.get(); err != nil {
		return nil, err
	}
	return c.c.NewSession()
}

func (c *pooledConnection) NewAuthSession(username, password string) (Session, error) {
	if err := c.get(); err != nil {
		return nil, err
	}
	return c.c.NewAuthSession(username, password)
}

// The above has been unabashedly copied from the pool functionality in redigo.

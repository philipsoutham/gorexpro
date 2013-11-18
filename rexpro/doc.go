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

// Connections
//
// The Conn interface is the primary interface for working with RexPro.
// Applications create connections by calling the Dial, DialWithTimeout or
// NewConn functions.
//
// The application must call the connection Close method when the application
// is done with the connection.
//
// Executing Commands
//
// The Conn interface has a generic method for executing Gremlin commands using rexpro:
//
//  DoScript(script string, bindings map[string]interface{}) (reply interface{}, err error)
//
// Concurrency
//
// Connections support a single concurrent caller to the `DoScript` method.
// For full concurrent access to RexPro, use the thread-safe Pool to get and
// release connections from within a goroutine.
//
// Usage - Simple
//
//  func main() {
//  	var properties = map[string]interface{}{"properties": map[string]interface{}{"foo": "bar", "score": 5}}
//  	if c, err := rexpro.Dial("your-host:8184", "your-graph-name"); err == nil {
//  		defer c.Close()
//  		if resp, err := c.DoScript("g.addVertex(properties)", properties); err == nil {
//  			doSomethingWith(resp)
//  		}
//  	}
//  }
//
// Usage - Connection Pools
//
//  var RexproPool *rexpro.Pool
//
//  func init() {
//  	RexproPool = &rexpro.Pool{
//  		MaxIdle:     5,
//  		MaxActive:   200,
//  		IdleTimeout: 25 * time.Second,
//  		Dial: func() (rexpro.Conn, error) {
//  			return rexpro.Dial("your-host:8184", "your-graph-name")
//  		},
//  		TestOnBorrow: func(c rexpro.Conn, t time.Time) error {
//  			_, err := c.DoScript("1", map[string]interface{}{})
//  			return err
//  		},
//  	}
//  }
//
//  func main() {
//  	var (
//  		c = RexproPool.Get()
//  		properties = map[string]interface{}{"properties": map[string]interface{}{"foo": "bar", "score": 5}}
//  	)
//  	defer c.Close()
//  	if resp, err := c.DoScript("g.addVertex(properties)", properties); err == nil {
//  		doSomethingWith(resp)
//  	}
//  }
package rexpro

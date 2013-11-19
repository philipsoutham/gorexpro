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
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/go-contrib/uuid"
	"github.com/ugorji/go/codec"
	"io"
	"net"
	"reflect"
	"sync"
	"time"
)

type MessageType byte
type SerializerType byte

// Serializer Definitions
const (
	MSGPACK MessageType = iota
	JSON
)

// Message Definitions
const (
	ERROR MessageType = iota
	SESSION_REQUEST
	SESSION_RESPONSE
	SCRIPT_REQUEST
	_
	SCRIPT_RESPONSE
)

var (
	defaultSendHeader = [...]byte{
		1,          // protocol version
		0,          // serializer type (msgpack)
		0, 0, 0, 0, // reserved
	}
	sessionlessUuid = [...]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
)

type msgHeader struct {
	Version        byte
	SerializerType byte
	Reserved       [4]byte
	MessageType    MessageType
	BodySize       uint32
}

type rexpro struct {
	graphName    string
	conn         net.Conn
	mu           sync.Mutex
	rw           *bufio.ReadWriter
	readTimeout  time.Duration
	writeTimeout time.Duration
}
type session struct {
	username string
	password string
	r        *rexpro
	sId      []byte
}

type sendReceiveScriptMsgArg struct {
	sId      []byte
	script   string
	bindings map[string]interface{}
}

type Session interface {
	// Begin starts a new session on the rexpro server
	Begin() error

	// Close closes the session at the rexpro server while leaving the
	// connection intact.
	Close() error

	// DoScript sends a command and bindings to the server and
	// returns the received reply.
	DoScript(string, map[string]interface{}) ([]interface{}, error)
}

type Conn interface {
	// Closes the connection.
	Close() error

	// DoScript sends a command and bindings to the server and
	// returns the received reply.
	DoScript(string, map[string]interface{}) ([]interface{}, error)

	// NewSession creates a new rexpro session handler
	NewSession() (Session, error)

	// NewAuthSession creates a new authenticated rexpro session handler
	NewAuthSession(string, string) (Session, error)
}

// Dial connects to the rexpro server
func Dial(address string, graphName string) (Conn, error) {
	c, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return NewConn(c, graphName, 0, 0), nil
}

// DialTimeout acts like Dial but takes timeouts for establishing the
// connection to the server, writing a command and reading a reply.
func DialTimeout(address string, graphName string, connectTimeout, readTimeout, writeTimeout time.Duration) (Conn, error) {
	var c net.Conn
	var err error
	if connectTimeout > 0 {
		c, err = net.DialTimeout("tcp", address, connectTimeout)
	} else {
		c, err = net.Dial("tcp", address)
	}
	if err != nil {
		return nil, err
	}
	return NewConn(c, address, readTimeout, writeTimeout), nil
}

// NewConn returns a new rexpro connection for the given net connection.
func NewConn(netConn net.Conn, graphName string, readTimeout, writeTimeout time.Duration) Conn {
	return &rexpro{
		graphName:    graphName,
		conn:         netConn,
		rw:           bufio.NewReadWriter(bufio.NewReader(netConn), bufio.NewWriter(netConn)),
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
	}
}

func (r *rexpro) NewSession() (Session, error) {
	return &session{r: r, sId: sessionlessUuid[:]}, nil
}

func (r *rexpro) NewAuthSession(username, password string) (Session, error) {
	return &session{
		r:        r,
		sId:      sessionlessUuid[:],
		username: username,
		password: password,
	}, nil
}

func (r *rexpro) Close() (err error) {
	r.mu.Lock()
	err = r.conn.Close()
	r.mu.Unlock()
	return
}

func (r *rexpro) DoScript(script string, bindings map[string]interface{}) (i []interface{}, e error) {
	r.mu.Lock()
	args := &sendReceiveScriptMsgArg{
		sessionlessUuid[:],
		script,
		bindings,
	}
	i, e = r.sendReceiveScriptMsg(args)
	r.mu.Unlock()
	return
}

func (s *session) Begin() (err error) {
	s.r.mu.Lock()
	err = s.createOrKillSession(false)
	s.r.mu.Unlock()
	return
}

func (s *session) createOrKillSession(kill bool) (err error) {
	s.r.rw.Writer.Write(defaultSendHeader[:])
	s.r.rw.Writer.WriteByte(byte(SESSION_REQUEST))
	msgBody, err := s.sessionBody(kill)
	if err != nil {
		return err
	}
	s.r.rw.Writer.Write(int2byte(binary.Size(msgBody)))
	s.r.rw.Writer.Write(msgBody)
	s.r.rw.Writer.Flush()

	var h = new(msgHeader)
	if err = binary.Read(s.r.rw.Reader, binary.BigEndian, h); err != nil {
		return
	}

	respMsg := make([]byte, h.BodySize)
	if c, err := io.ReadFull(s.r.rw.Reader, respMsg); err != nil || uint32(c) != h.BodySize {
		return fmt.Errorf("rexpro: read body -> only read %d bytes out of %d, %s", c, h.BodySize, err)
	}

	resp, err := decodeBody(respMsg)
	if err != nil {
		return err
	}
	s.sId = (resp[0].([]byte))

	if h.MessageType != SESSION_RESPONSE {
		err = fmt.Errorf("rexpro: Got msg type %d, expecting %d", h.MessageType, SESSION_RESPONSE)
	}
	return
}

func (s *session) Close() (err error) {
	s.r.mu.Lock()
	err = s.createOrKillSession(true)
	s.r.mu.Unlock()
	return
}

func (s *session) DoScript(script string, bindings map[string]interface{}) (i []interface{}, e error) {
	s.r.mu.Lock()
	args := &sendReceiveScriptMsgArg{
		s.sId,
		script,
		bindings,
	}
	i, e = s.r.sendReceiveScriptMsg(args)
	s.r.mu.Unlock()
	return
}

func (r *rexpro) writeMsg(msgType MessageType, body []byte) (err error) {
	var c = 0
	if c, err = r.rw.Writer.Write(defaultSendHeader[:]); err == nil && c == len(defaultSendHeader) {
		if err = r.rw.Writer.WriteByte(byte(msgType)); err == nil {
			bodyLen := binary.Size(body)
			if c, err = r.rw.Writer.Write(int2byte(bodyLen)); err == nil && c == 4 {
				if c, err = r.rw.Writer.Write(body); err == nil && c == bodyLen {
					err = r.rw.Writer.Flush()
				} else if err == nil {
					err = fmt.Errorf("rexpro: write body -> Incomplete write %d != %d", c, bodyLen)
				}
			} else if err == nil {
				err = fmt.Errorf("rexpro: write body length -> Incomplete write %d != 4", c)
			}
		}
	} else if err == nil {
		err = fmt.Errorf("rexpro: write header -> Incomplete write %d != %d", c, len(defaultSendHeader))
	}
	return
}

func (r *rexpro) readMsg(expectedMsgType MessageType) ([]interface{}, error) {
	var h = new(msgHeader)
	if err := binary.Read(r.rw.Reader, binary.BigEndian, h); err != nil {
		return nil, err
	}

	respMsg := make([]byte, h.BodySize)
	if c, err := io.ReadFull(r.rw.Reader, respMsg); err != nil || uint32(c) != h.BodySize {
		return nil, fmt.Errorf("rexpro: read body -> only read %d bytes out of %d, %s", c, h.BodySize, err)
	}

	resp, err := decodeBody(respMsg)
	if err != nil {
		return nil, err
	}
	if h.MessageType != expectedMsgType {
		msg := "That's all I know."
		if h.MessageType == ERROR {
			msg = string(resp[3].([]byte))
		}
		return nil, fmt.Errorf("rexpro: Got msg type %d, expected %d\n%s", h.MessageType, expectedMsgType, msg)
	}
	return resp, nil
}

func (r *rexpro) sendReceiveScriptMsg(a *sendReceiveScriptMsgArg) ([]interface{}, error) {
	msgBody, err := scriptBody(a.sId, r.graphName, a.script, a.bindings)
	if err != nil {
		return nil, err
	}
	if err := r.writeMsg(SCRIPT_REQUEST, msgBody); err != nil {
		return nil, err
	}
	return r.readMsg(SCRIPT_RESPONSE)
}

func (s *session) sessionBody(kill bool) (out []byte, err error) {
	var (
		mh    = new(codec.MsgpackHandle)
		enc   = codec.NewEncoderBytes(&out, mh)
		reqId = uuid.NewV4()
	)
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	err = enc.Encode([]interface{}{
		s.sId,
		reqId[:],
		map[string]interface{}{
			"graphName":    s.r.graphName,
			"graphObjName": "g",
			"killSession":  kill,
		},
		s.username,
		s.password,
	})
	return
}

func scriptBody(sessionId []byte, graphName, script string, bindings map[string]interface{}) (out []byte, err error) {
	var (
		mh            = new(codec.MsgpackHandle)
		enc           = codec.NewEncoderBytes(&out, mh)
		reqId         = uuid.NewV4()
		isSessionless = bytes.Equal(sessionId, sessionlessUuid[:])
	)
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	meta := map[string]interface{}{
		"inSession":    !isSessionless,
		"isolate":      isSessionless,
		"graphObjName": "g",
	}
	if isSessionless {
		meta["graphName"] = graphName
	}
	err = enc.Encode([]interface{}{
		sessionId,
		reqId[:],
		meta,
		"groovy",
		script,
		bindings,
	})
	return
}

// func byte2int(val []byte) int {
// 	return (int(val[0])<<24)&0xFF000000 | (int(val[1])<<16)&0xFF0000 | (int(val[2])<<8)&0xFF00 | (int(val[3])<<0)&0xFF
// }

func int2byte(val int) []byte {
	return []byte{
		byte(val >> 24),
		byte(val >> 16),
		byte(val >> 8),
		byte(val >> 0 & 0xFF),
	}

}

func decodeBody(body []byte) (retVal []interface{}, err error) {
	var mh = new(codec.MsgpackHandle)
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	dec := codec.NewDecoderBytes(body, mh)
	if e := dec.Decode(&retVal); e != nil {
		err = errors.New("rexpro: msgpack decode error")
	}
	return
}

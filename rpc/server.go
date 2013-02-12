package rpc

import (
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
)

// A ServerCodec implements reading of RPC requests and writing of RPC
// responses for the server side of an RPC session.  The server calls
// ReadRequestHeader and ReadRequestBody in pairs to read requests from
// the connection, and it calls WriteResponse to write a response back.
// The server calls Close when finished with the connection.
// ReadRequestBody may be called with a nil argument to force the body of
// the request to be read and discarded.
type ServerCodec interface {
	ReadRequestHeader(*Request) error
	ReadRequestBody(interface{}) error
	WriteResponse(*Response, interface{}) error
}

// Request is a header written before every RPC call.
type Request struct {
	// RequestId holds the sequence number of the request.
	RequestId uint64

	// Type holds the type of object to act on.
	Type string

	// Id holds the id of the object to act on.
	Id string

	// Action holds the action to invoke on the remote object.
	Action string
}

// Response is a header written before every RPC return.
type Response struct {
	// RequestId echoes that of the request.
	RequestId uint64

	// Error holds the error, if any.
	Error string
}

// codecServer represents an active server instance.
type codecServer struct {
	*Server
	codec ServerCodec

	// req holds the most recently read request header.
	req Request

	// doneReadBody is true if the body of
	// the request has been read.
	doneReadBody bool

	// root holds the root value being served.
	root reflect.Value
}

// Accept accepts connections on the listener and serves requests for
// each incoming connection.  The newRoot function is called
// to create the root value for the connection before spawning
// the goroutine to service the RPC requests; it may be nil,
// in which case the original root value passed to NewServer
// will be used. A codec is chosen for the connection by
// calling newCodec.
//
// Accept blocks; the caller typically invokes it in
// a go statement.
func (srv *Server) Accept(l net.Listener,
	newRoot func(net.Conn) (interface{}, error),
	newCodec func(io.ReadWriter) ServerCodec) error {
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		rootv := srv.root
		if newRoot != nil {
			root, err := newRoot(c)
			if err != nil {
				log.Printf("rpc: connection refused: %v", err)
				c.Close()
				continue
			}
			rootv = reflect.ValueOf(root)
		}
		go func() {
			defer c.Close()
			if err := srv.serve(rootv, newCodec(c)); err != nil {
				log.Printf("rpc: ServeCodec error: %v", err)
			}
		}()
	}
	panic("unreachable")
}

// ServeCodec runs the server on a single connection.  ServeCodec
// blocks, serving the connection until the client hangs up.  The caller
// typically invokes ServeCodec in a go statement.  The given
// root value, which must be the same type as that passed to
// NewServer, is used to invoke the RPC requests. If rootValue
// nil, the original root value passed to NewServer will
// be used instead.
func (srv *Server) ServeCodec(codec ServerCodec, rootValue interface{}) error {
	return srv.serve(reflect.ValueOf(rootValue), codec)
}

func (srv *Server) serve(root reflect.Value, codec ServerCodec) error {
	// TODO(rog) allow concurrent requests.
	if root.Type() != srv.root.Type() {
		panic(fmt.Errorf("rpc: unexpected type of root value; got %s, want %s", root.Type(), srv.root.Type()))
	}
	csrv := &codecServer{
		Server: srv,
		codec:  codec,
		root:   root,
	}
	for {
		csrv.req = Request{}
		err := codec.ReadRequestHeader(&csrv.req)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		csrv.doneReadBody = false
		rv, err := csrv.runRequest()
		if err != nil {
			if !csrv.doneReadBody {
				_ = codec.ReadRequestBody(nil)
			}
			resp := &Response{
				RequestId: csrv.req.RequestId,
			}
			resp.Error = err.Error()
			if err := codec.WriteResponse(resp, nil); err != nil {
				return err
			}
			continue
		}
		var rvi interface{}
		if rv.IsValid() {
			rvi = rv.Interface()
		}
		resp := &Response{
			RequestId: csrv.req.RequestId,
		}
		if err := codec.WriteResponse(resp, rvi); err != nil {
			return err
		}
	}
	panic("unreachable")
}

func (csrv *codecServer) readRequestBody(arg interface{}) error {
	csrv.doneReadBody = true
	return csrv.codec.ReadRequestBody(arg)
}

func (csrv *codecServer) runRequest() (reflect.Value, error) {
	o := csrv.obtain[csrv.req.Type]
	if o == nil {
		return reflect.Value{}, fmt.Errorf("unknown object type %q", csrv.req.Type)
	}
	obj, err := o.call(csrv.root, csrv.req.Id)
	if err != nil {
		return reflect.Value{}, err
	}
	a := csrv.action[o.ret][csrv.req.Action]
	if a == nil {
		return reflect.Value{}, fmt.Errorf("no such action %q on %s", csrv.req.Action, csrv.req.Type)
	}
	var arg reflect.Value
	if a.arg == nil {
		// If the action has no arguments, discard any RPC parameters.
		if err := csrv.readRequestBody(nil); err != nil {
			return reflect.Value{}, err
		}
	} else {
		argp := reflect.New(a.arg)
		if err := csrv.readRequestBody(argp.Interface()); err != nil {
			return reflect.Value{}, err
		}
		arg = argp.Elem()
	}
	return a.call(obj, arg)
}
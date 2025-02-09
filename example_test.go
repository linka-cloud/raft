package raft_test

import (
	"io"
	"net/http"

	"google.golang.org/grpc"

	"go.linka.cloud/raft"
	"go.linka.cloud/raft/transport"
	"go.linka.cloud/raft/transport/raftgrpc"
	"go.linka.cloud/raft/transport/rafthttp"
)

type stateMachine struct{}

func (stateMachine) Apply([]byte) error                     { return nil }
func (stateMachine) Snapshot() (r io.ReadCloser, err error) { return }
func (stateMachine) Restore(io.ReadCloser) (err error)      { return }

func Example_gRPC() {
	srv := grpc.NewServer()
	node := raft.NewNode(stateMachine{}, transport.GRPC)
	raftgrpc.RegisterHandler(srv, node.Handler())
}

func Example_http() {
	node := raft.NewNode(stateMachine{}, transport.HTTP)
	handler := rafthttp.Handler(node.Handler())
	_ = http.Server{
		Handler: handler,
	}
}

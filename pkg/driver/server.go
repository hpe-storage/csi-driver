// Copyright 2019 Hewlett Packard Enterprise Development LP
// Copyright 2017 The Kubernetes Authors.

package driver

import (
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/hpe-storage/common-host-libs/logger"
)

// NonBlockingGRPCServer defines the non-blocking gRPC server interfaces
type NonBlockingGRPCServer interface {
	// Start services at the endpoint
	Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer)
	// Stops the service gracefully
	GracefulStop()
	// Stops the service forcefully
	Stop()
}

// NewNonBlockingGRPCServer returns a non-blocking gRPC server
func NewNonBlockingGRPCServer() NonBlockingGRPCServer {
	return &nonBlockingGRPCServer{}
}

// NonBlocking server
type nonBlockingGRPCServer struct {
	server *grpc.Server
}

func (s *nonBlockingGRPCServer) Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {
	go s.serve(endpoint, ids, cs, ns)
}

func (s *nonBlockingGRPCServer) GracefulStop() {
	s.server.GracefulStop()
}

func (s *nonBlockingGRPCServer) Stop() {
	s.server.Stop()
}

func (s *nonBlockingGRPCServer) serve(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {

	proto, addr, err := ParseEndpoint(endpoint)
	if err != nil {
		log.Errorf(err.Error())
		return
	}

	if proto == "unix" {
		addr = "/" + addr
		if err = os.Remove(addr); err != nil && !os.IsNotExist(err) {
			log.Errorf("Failed to remove %s, error: %s", addr, err.Error())
			return
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		log.Errorf("Failed to listen: %v", err)
		return
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logGRPC),
	}
	server := grpc.NewServer(opts...)
	s.server = server

	if ids != nil {
		csi.RegisterIdentityServer(server, ids)
	}
	if cs != nil {
		csi.RegisterControllerServer(server, cs)
	}
	if ns != nil {
		csi.RegisterNodeServer(server, ns)
	}

	log.Infof("Listening for connections on address: %#v", listener.Addr())

	server.Serve(listener)
}

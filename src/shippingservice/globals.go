package main

import (
	"google.golang.org/grpc"

	pb "github.com/GoogleCloudPlatform/microservices-demo/src/shippingservice/genproto"
)

// server controls RPC service responses.
type server struct {
	pb.UnimplementedShippingServiceServer

	delayStoreAddr string
	delayStoreConn *grpc.ClientConn

	speedMap map[string]bool
	delayMap map[string]int

	defaultDelay int
}

var svc server

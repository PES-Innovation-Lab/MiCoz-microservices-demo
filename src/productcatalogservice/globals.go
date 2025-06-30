package main

import (
	pb "github.com/GoogleCloudPlatform/microservices-demo/src/productcatalogservice/genproto"
	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedProductCatalogServiceServer
	catalog pb.ListProductsResponse

	delayStoreAddr string
	delayStoreConn *grpc.ClientConn

	speedMap map[string]bool
	delayMap map[string]int

	defaultDelay int
}

var svc server

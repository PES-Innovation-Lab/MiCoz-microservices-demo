package main

import (
	"strconv"
	"time"
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/GoogleCloudPlatform/microservices-demo/src/checkoutservice/genproto"
)

func mustConnGRPC(ctx context.Context, conn **grpc.ClientConn, addr string) {
	var err error
	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()
	*conn, err = grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(
			otelgrpc.UnaryClientInterceptor(),
			outgoingRequestInterceptor,
		),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()))
	if err != nil {
		panic(errors.Wrapf(err, "grpc: failed to connect %s", addr))
	}
}

type DelayContext struct {
	LocalDelay int
	Endpoint   string
}

const DelayCtxKey = "delayCtx"

func parseMD (ctx context.Context) (int, string, error) {
	var delay int
	var endpoint string
	var err error

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Warnf("self called without receiving any metadata")
	}

	localDelayValues := md.Get("localDelay")
	if len(localDelayValues) == 0 {
		delay = 0
	} else {
		delay, err = strconv.Atoi(localDelayValues[0])
		if err != nil {
			delay = 0
			log.Warnf("invalid localDelay: %v", err)
		}
	}

	endpointArr := md.Get("endpoint")
	if len(endpointArr) == 0 {
		return 0, "", fmt.Errorf("No endpoint in metadata")
	}

	endpoint = endpointArr[0]

	return delay, endpoint, nil
}


func parseCtx(ctx context.Context) (*DelayContext, error) {
	delayCtx, ok := ctx.Value(DelayCtxKey).(*DelayContext)
	if !ok {
		return nil, fmt.Errorf("no delay context in context")
	}
	return delayCtx, nil
}

func syncDelays (ctx context.Context, delayCtx *DelayContext) error {
	globalDelay, err := getGlobalDelay(ctx)
	if err != nil {
		return err
	}

	if globalDelay > delayCtx.LocalDelay {
		sleepDuration := time.Duration(globalDelay - delayCtx.LocalDelay)

		time.Sleep(sleepDuration * time.Millisecond)
		delayCtx.LocalDelay += int(sleepDuration)
	}

	return nil
}

func getDelay (method string) int {
	delay, ok := svc.delayMap[method]
	if ok {
		return delay
	}

	if svc.defaultDelay == 0 {
		delay = 10
	} else {
		delay = svc.defaultDelay
	}

	return delay
}

func incomingRequestInterceptor(ctx context.Context, req any, serverInfo *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	localDelay, endpoint, err := parseMD(ctx)
	if err != nil {
		resp, rpcErr := handler(ctx, req)
		return resp, rpcErr
	}

	if _, exists := svc.processedRequests.LoadOrStore(endpoint, true); exists {
		resp, rpcErr := handler(ctx, req)
		return resp, rpcErr
	}

	delayCtx := &DelayContext{
		LocalDelay: localDelay,
		Endpoint:   endpoint,
	}

	toSpeed, ok := svc.speedMap[serverInfo.FullMethod]
	if !ok {
		toSpeed = false
	} else {
		localDelay += getDelay(serverInfo.FullMethod)
	}

	ctx = context.WithValue(ctx, DelayCtxKey, delayCtx)
	if toSpeed {
		if err = incGlobalDelay(ctx, serverInfo.FullMethod); err != nil {
			return nil, err
		}
	}

	resp, rpcErr := handler(ctx, req)

	err = syncDelays(ctx, delayCtx)
	if err != nil {
		return resp, err
	}

	grpc.SetTrailer(ctx, metadata.Pairs("localDelay", strconv.Itoa(delayCtx.LocalDelay)))

	return resp, rpcErr
}

func outgoingRequestInterceptor(ctx context.Context, method string, req, reply any, clientConn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	delayCtx, err := parseCtx(ctx)
	if err != nil {
		return err
	}

	ctx = metadata.AppendToOutgoingContext(ctx,
		"localDelay", strconv.Itoa(delayCtx.LocalDelay),
		"endpoint", delayCtx.Endpoint,
	)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	rpcErr := invoker(ctx, method, req, reply, clientConn, opts...)

	if delayVals := trailer.Get("localDelay"); len(delayVals) > 0 {
		if calleeDelay, err := strconv.Atoi(delayVals[0]); err == nil {

			if calleeDelay > delayCtx.LocalDelay {
				delayCtx.LocalDelay = calleeDelay
			}
		}
	}

	return rpcErr
}

func getGlobalDelay(ctx context.Context) (int, error) {
	delayCtx, err := parseCtx(ctx)
	if err != nil {
		return 0, err
	}

	resp, err := pb.NewDelayStoreClient(svc.delayStoreConn).GetGlobalDelay(ctx,
		&pb.GetDelayRequest{Endpoint: delayCtx.Endpoint},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get global delay: %w", err)
	}
	return int(resp.GetGlobalDelay()), nil
}

func incGlobalDelay(ctx context.Context, method string) error {
	delayCtx, err := parseCtx(ctx)
	if err != nil {
		return err
	}

	_, err = pb.NewDelayStoreClient(svc.delayStoreConn).IncGlobalDelay(ctx,
		&pb.IncDelayRequest{DelaySize: int32(getDelay(method)), Endpoint: delayCtx.Endpoint},
	)
	return err
}

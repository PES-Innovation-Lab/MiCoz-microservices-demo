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

	pb "github.com/GoogleCloudPlatform/microservices-demo/src/frontend/genproto"
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

func outgoingRequestInterceptor(ctx context.Context, method string, req, reply any, clientConn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	var count bool

	delayCtx, err := parseCtx(ctx)
	if err != nil {
		return err
	}

	if count {
		return nil
	}

	ctx = metadata.AppendToOutgoingContext(ctx,
		"localDelay", strconv.Itoa(delayCtx.LocalDelay),
		"endpoint", delayCtx.Endpoint,
	)

	var trailer metadata.MD
	opts = append(opts, grpc.Trailer(&trailer))

	count, ok := ctx.Value("count").(bool)
	if !ok {
		ctx = context.WithValue(ctx, "count", true)
	}

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

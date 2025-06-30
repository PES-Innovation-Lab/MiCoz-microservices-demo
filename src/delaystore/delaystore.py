import os
import time
from concurrent import futures
import threading

from google.auth.exceptions import DefaultCredentialsError
import grpc

import demo_pb2
import demo_pb2_grpc
from grpc_health.v1 import health_pb2
from grpc_health.v1 import health_pb2_grpc

from opentelemetry import trace
from opentelemetry.instrumentation.grpc import GrpcInstrumentorClient, GrpcInstrumentorServer
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter

from flask import Flask, request

from logger import getJSONLogger
logger = getJSONLogger('delaystore-server')

# Initial default is 0
GlobalDelayMap = {}
app = Flask(__name__)

lock = threading.Lock()

def peek_map(map: dict, key: str):
    try:
        notNone = map[key] is not None
    except KeyError:
        notNone = False

    return notNone

class DelayStore (demo_pb2_grpc.DelayStoreServicer):
    def GetGlobalDelay(self, request, context):
        global GlobalDelayMap

        response = demo_pb2.GetDelayResponse()

        if peek_map(GlobalDelayMap, request.endpoint):
            print(f"Responding with global delay of {request.endpoint}, delay: {GlobalDelayMap[request.endpoint]}")
            response.global_delay = GlobalDelayMap[request.endpoint]
        else:
            response.global_delay = 0

        return response

    def IncGlobalDelay(self, request, context):
        global GlobalDelayMap

        response = demo_pb2.Empty()

        with lock:
            if peek_map(GlobalDelayMap, request.endpoint):
                print(f"Incrementing global delay for endpoint: {request.endpoint} with {request.delay_size}")
                GlobalDelayMap[request.endpoint] += request.delay_size
            else:
                GlobalDelayMap[request.endpoint] = request.delay_size

        return response

    def Check(self, request, context):
        return health_pb2.HealthCheckResponse(
            status=health_pb2.HealthCheckResponse.SERVING)

    def Watch(self, request, context):
        return health_pb2.HealthCheckResponse(
            status=health_pb2.HealthCheckResponse.UNIMPLEMENTED)

def start_flask(http_port: str):
    logger.info(f"Listerning to http on port: {http_port}")
    app.run(port=http_port)

if __name__ == "__main__":
    logger.info("initializing delaystore")

    try:
      grpc_client_instrumentor = GrpcInstrumentorClient()
      grpc_client_instrumentor.instrument()
      grpc_server_instrumentor = GrpcInstrumentorServer()
      grpc_server_instrumentor.instrument()
      if os.environ["ENABLE_TRACING"] == "1":
        trace.set_tracer_provider(TracerProvider())
        otel_endpoint = os.getenv("COLLECTOR_SERVICE_ADDR", "localhost:4317")
        trace.get_tracer_provider().add_span_processor(
          BatchSpanProcessor(
              OTLPSpanExporter(
              endpoint = otel_endpoint,
              insecure = True
            )
          )
        )
    except (KeyError, DefaultCredentialsError):
        logger.info("Tracing disabled.")

    grpc_port = os.environ.get('GRPCPORT', "5000")
    http_port = os.environ.get('HTTPPORT', "8000")

    thread = threading.Thread(target=start_flask, args=(http_port,))
    thread.start()

    # create gRPC server
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    # add class to gRPC server
    service = DelayStore()
    demo_pb2_grpc.add_DelayStoreServicer_to_server(service, server)
    health_pb2_grpc.add_HealthServicer_to_server(service, server)

    # start server
    logger.info("listening on grpc_port: " + grpc_port)
    server.add_insecure_port('[::]:'+grpc_port)
    server.start()

    # keep alive
    try:
         while True:
            time.sleep(10000)
    except KeyboardInterrupt:
            server.stop(0)

@app.route('/')
def home():
    return 'Welcome to speed controller!'

@app.route('/getglobaldelay/<endpoint>')
def get_GlobalDelay():
    endpoint = request.args.get('endpoint')

    if peek_map(GlobalDelayMap, endpoint):
        return f'{GlobalDelayMap[endpoint]}'

    return '0'

@app.route('/clearstate')
def clear_GlobalDelay():
    global GlobalDelayMap
    GlobalDelayMap = {}
    return "ACK"

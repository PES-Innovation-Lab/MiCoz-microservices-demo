pip3 install -r requirements.txt
python -m grpc_tools.protoc -I../../protos --python_out=. --grpc_python_out=. ../../protos/demo.proto
